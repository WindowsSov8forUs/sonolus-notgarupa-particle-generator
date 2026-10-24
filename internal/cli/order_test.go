package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOrderDB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.json")
	in := `{"info":{"title":{"en":"x"}},"skins":[{"name":"b","v":1},{"name":"z"},{"name":"a","t":"<&>"}],"effects":[]}`
	if err := os.WriteFile(path, []byte(in), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := orderDB(path, []string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	want := `{"info":{"title":{"en":"x"}},"skins":[{"name":"a","t":"<&>"},{"name":"b","v":1},{"name":"z"}],"effects":[]}`
	if string(got) != want {
		t.Errorf("got %s", got)
	}
}
