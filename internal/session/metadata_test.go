package session

import (
	"context"
	"reflect"
	"testing"

	"google.golang.org/adk/session"
)

func TestFileService_SetNameAndGetMetadata(t *testing.T) {
	dir := t.TempDir()
	fs, err := NewFileService(dir)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := fs.Create(context.Background(), &session.CreateRequest{
		AppName: "go-pi", UserID: "u1",
	})
	if err != nil {
		t.Fatal(err)
	}
	id := resp.Session.ID()
	if err := fs.SetName(id, "go-pi", "u1", "my-branch"); err != nil {
		t.Fatal(err)
	}
	meta, err := fs.GetMetadata(id, "go-pi", "u1")
	if err != nil {
		t.Fatal(err)
	}
	if meta.Name != "my-branch" {
		t.Fatalf("name = %q", meta.Name)
	}
}

func TestFileService_SetTags_Dedupes(t *testing.T) {
	dir := t.TempDir()
	fs, _ := NewFileService(dir)
	resp, _ := fs.Create(context.Background(), &session.CreateRequest{AppName: "go-pi", UserID: "u1"})
	id := resp.Session.ID()
	if err := fs.SetTags(id, "go-pi", "u1", []string{"a", "b", "a", "c", "b"}); err != nil {
		t.Fatal(err)
	}
	meta, _ := fs.GetMetadata(id, "go-pi", "u1")
	if !reflect.DeepEqual(meta.Tags, []string{"a", "b", "c"}) {
		t.Fatalf("tags = %+v", meta.Tags)
	}
}

func TestFileService_GetMetadata_OldSessionWithoutFields(t *testing.T) {
	dir := t.TempDir()
	fs, _ := NewFileService(dir)
	resp, _ := fs.Create(context.Background(), &session.CreateRequest{AppName: "go-pi", UserID: "u1"})
	id := resp.Session.ID()
	meta, err := fs.GetMetadata(id, "go-pi", "u1")
	if err != nil {
		t.Fatal(err)
	}
	if meta.Name != "" || len(meta.Tags) != 0 {
		t.Fatalf("expected empty: %+v", meta)
	}
}
