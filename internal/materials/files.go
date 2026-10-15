package materials

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"unicode/utf8"
)

const MaxFileBytes = 10 << 20
const MaxTotalBytes = 20 << 20
const MaxFiles = 8
const MaxTextBytes = 256 << 10

type Upload struct {
	Name string `json:"name"`
	Data string `json:"data"`
}
type File struct {
	Name      string `json:"name"`
	MediaType string `json:"mediaType"`
	Size      int    `json:"size"`
	SHA256    string `json:"sha256"`
	Text      string `json:"text,omitempty"`
	Data      string `json:"data,omitempty"`
}

func (f File) Metadata() File { f.Text = ""; f.Data = ""; return f }
func Prepare(uploads []Upload) ([]File, error) {
	if len(uploads) > MaxFiles {
		return nil, errors.New("每次最多上传 8 个附件")
	}
	out := []File{}
	total := 0
	for _, u := range uploads {
		if u.Name == "" || len(u.Name) > 240 || strings.ContainsAny(u.Name, "/\\\x00\r\n") || u.Name == "." || u.Name == ".." {
			return nil, errors.New("附件名称无效")
		}
		if len(u.Data) > base64.StdEncoding.EncodedLen(MaxFileBytes) {
			return nil, errors.New("单个附件最多 10 MiB")
		}
		raw, err := base64.StdEncoding.DecodeString(u.Data)
		if err != nil {
			return nil, errors.New("附件编码无效")
		}
		if len(raw) > MaxFileBytes {
			return nil, errors.New("单个附件最多 10 MiB")
		}
		total += len(raw)
		if total > MaxTotalBytes {
			return nil, errors.New("附件总量最多 20 MiB")
		}
		if len(raw) == 0 {
			return nil, errors.New("附件为空")
		}
		sum := sha256.Sum256(raw)
		f := File{Name: u.Name, Size: len(raw), SHA256: hex.EncodeToString(sum[:])}
		mime := http.DetectContentType(raw)
		switch {
		case strings.EqualFold(path.Ext(u.Name), ".docx"):
			f.Text, err = docxText(raw)
			f.MediaType = "text/plain"
		case bytes.HasPrefix(raw, []byte("%PDF-")):
			f.MediaType = "application/pdf"
			f.Data = u.Data
		case mime == "image/png" || mime == "image/jpeg" || mime == "image/gif" || mime == "image/webp":
			f.MediaType = mime
			f.Data = u.Data
		case utf8.Valid(raw) && !bytes.ContainsRune(raw, 0):
			if len(raw) > MaxTextBytes {
				return nil, errors.New("文本附件最多 256 KiB；请拆分材料")
			}
			f.MediaType = "text/plain"
			f.Text = string(raw)
		default:
			return nil, fmt.Errorf("附件 %s 格式不支持；可上传文本、DOCX、PDF、PNG/JPEG/GIF/WebP", u.Name)
		}
		if err != nil {
			return nil, fmt.Errorf("附件 %s：%w", u.Name, err)
		}
		out = append(out, f)
	}
	return out, nil
}
func docxText(raw []byte) (string, error) {
	z, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return "", errors.New("无效 DOCX")
	}
	for _, f := range z.File {
		if f.Name != "word/document.xml" {
			continue
		}
		if f.UncompressedSize64 > 4<<20 {
			return "", errors.New("DOCX 正文超限")
		}
		r, err := f.Open()
		if err != nil {
			return "", err
		}
		defer r.Close()
		dec := xml.NewDecoder(io.LimitReader(r, 4<<20))
		var b strings.Builder
		inText := false
		for {
			tok, err := dec.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				return "", err
			}
			switch t := tok.(type) {
			case xml.StartElement:
				if t.Name.Local == "t" {
					inText = true
				}
				if t.Name.Local == "tab" {
					b.WriteByte('\t')
				}
			case xml.CharData:
				if inText {
					b.Write(t)
				}
			case xml.EndElement:
				if t.Name.Local == "t" {
					inText = false
				}
				if t.Name.Local == "p" {
					b.WriteByte('\n')
				}
			}
			if b.Len() > MaxTextBytes {
				return "", errors.New("DOCX 提取文本超过 256 KiB")
			}
		}
		if strings.TrimSpace(b.String()) == "" {
			return "", errors.New("DOCX 没有可提取正文；嵌入图片请单独上传")
		}
		return b.String(), nil
	}
	return "", errors.New("DOCX 缺少 word/document.xml")
}
