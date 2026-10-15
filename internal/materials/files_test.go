package materials

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
)

func upload(name string, data []byte) Upload {
	return Upload{Name: name, Data: base64.StdEncoding.EncodeToString(data)}
}
func TestPrepareDocumentsAndRejectInvalid(t *testing.T) {
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	w, _ := z.Create("word/document.xml")
	w.Write([]byte(`<w:document xmlns:w="urn:w"><w:p><w:r><w:t>合同正文</w:t></w:r></w:p></w:document>`))
	z.Close()
	files, err := Prepare([]Upload{upload("contract.docx", b.Bytes()), upload("paper.pdf", []byte("%PDF-1.4\nfixture")), upload("note.txt", []byte("文本"))})
	if err != nil || len(files) != 3 {
		t.Fatalf("%+v %v", files, err)
	}
	if !strings.Contains(files[0].Text, "合同正文") || files[1].MediaType != "application/pdf" || files[1].Data == "" || files[2].Text != "文本" {
		t.Fatalf("%+v", files)
	}
	for _, u := range []Upload{upload("../secret", []byte("a")), upload("empty", nil), upload("bad.docx", []byte("bad")), upload("binary", []byte{0, 1}), upload("huge.txt", bytes.Repeat([]byte("a"), MaxTextBytes+1)), {Name: "x", Data: "@@"}} {
		if _, err := Prepare([]Upload{u}); err == nil {
			t.Fatalf("accepted invalid %s", u.Name)
		}
	}
	oversizedPDF := append([]byte("%PDF-"), bytes.Repeat([]byte("x"), MaxFileBytes-4)...)
	if _, err := Prepare([]Upload{upload("too-large.pdf", oversizedPDF)}); err == nil {
		t.Fatal("decoded file size exceeds limit")
	}

	if _, err := Prepare(make([]Upload, 9)); err == nil {
		t.Fatal("accepted excess files")
	}
}
