package skills

import (
	"fmt"
	"sync"
	"testing"
)

func TestManagedCopiesUseNormalDiscoveryLoadAndSnapshotContracts(t *testing.T) {
	c, err := Discover(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	source := "---\nname: explain-example\ndescription: Explain using an example.\n---\nGive an example and verify the result.\n"
	copy, err := CopySnapshot(source, "0123456789ab")
	if err != nil {
		t.Fatal(err)
	}
	if err = c.RegisterManaged(copy); err != nil {
		t.Fatal(err)
	}
	if err = c.RegisterManaged(copy); err != nil {
		t.Fatal(err)
	}
	loaded, err := c.Load(copy.Name)
	if err != nil || loaded != copy.Content {
		t.Fatal(loaded, err)
	}
	snapshots, err := c.Snapshot(true, nil)
	if err != nil || len(snapshots) != 1 || snapshots[0].SHA256 != copy.SHA256 {
		t.Fatal(snapshots, err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				_ = c.Items()
				_ = c.Summary()
				_ = c.SummaryFor([]string{copy.Name})
				_, _ = c.Load(copy.Name)
				snap, e := CopySnapshot(source, fmt.Sprintf("%012x", i+1))
				if e != nil {
					t.Error(e)
					return
				}
				if e = c.RegisterManaged(snap); e != nil {
					t.Error(e)
					return
				}
			}
		}(i)
	}
	wg.Wait()
	if err = c.WithdrawManaged(copy.Name, "wrong hash"); err == nil {
		t.Fatal("wrong copy withdrawn")
	}
	if err = c.WithdrawManaged(copy.Name, copy.SHA256); err != nil {
		t.Fatal(err)
	}
	if c.Installed(copy.Name) {
		t.Fatal("withdrawn copy remains available")
	}
	if snapshots[0].Content != copy.Content {
		t.Fatal("frozen snapshot changed")
	}
}
