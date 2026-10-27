package cli

import (
	"encoding/json"
	"io"
	"os"
	"testing"

	"jgarner.dev/bud/internal/doctor"
)

func TestReportJSON(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout := os.Stdout
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = stdout })

	err = reportJSON([]doctor.Result{
		{Name: "git", Status: doctor.OK},
		{Name: "go", Status: doctor.Warn, Detail: "not found", Fix: "brew install go"},
		{Name: "nvim config", Status: doctor.Fail},
	})
	w.Close()
	os.Stdout = stdout
	// a failing check is in the JSON, not the exit code
	if err != nil {
		t.Fatalf("err = %v", err)
	}

	data, _ := io.ReadAll(r)
	var got []map[string]string
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("%v: %s", err, data)
	}
	if len(got) != 3 || got[0]["status"] != "ok" || got[1]["status"] != "warn" || got[1]["fix"] != "brew install go" || got[2]["status"] != "fail" {
		t.Errorf("got %v", got)
	}
	if _, ok := got[0]["detail"]; ok {
		t.Error("empty detail should be left out")
	}
}
