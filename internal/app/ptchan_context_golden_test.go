package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"martie/internal/gateway"
)

type ptchanContextGoldenMeta struct {
	TargetPostID int64 `json:"target_post_id"`
	MaxReplies   int   `json:"max_replies"`
}

func TestPtchanContextGoldenFiles(t *testing.T) {
	// Set MARTIE_UPDATE_GOLDEN=1 to refresh expected packets after an
	// intentional rendering change.
	fixtures, err := ptchanContextGoldenFixtures()
	if err != nil {
		t.Fatal(err)
	}
	if len(fixtures) == 0 {
		t.Fatal("no ptchan context golden fixtures found")
	}

	for _, fixture := range fixtures {
		name := strings.TrimSuffix(filepath.Base(fixture), filepath.Ext(fixture))
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(fixture)
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			var thread gateway.Thread
			if err := json.Unmarshal(raw, &thread); err != nil {
				t.Fatalf("decode fixture: %v", err)
			}

			meta := readPtchanContextGoldenMeta(t, strings.TrimSuffix(fixture, filepath.Ext(fixture))+".meta")
			cfg := PtchanContextConfig{MaxReplies: meta.MaxReplies}
			if cfg.MaxReplies == 0 {
				cfg.MaxReplies = defaultPtchanMaxReplies
			}

			got := formatPtchanContext(thread, meta.TargetPostID, cfg)
			golden := strings.TrimSuffix(fixture, filepath.Ext(fixture)) + ".golden"
			if os.Getenv("MARTIE_UPDATE_GOLDEN") == "1" {
				if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
					t.Fatalf("update golden: %v", err)
				}
			}

			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("read golden: %v", err)
			}
			if got != string(want) {
				t.Fatalf("rendered context differs from golden\n%s", firstStringDiff(string(want), got))
			}
		})
	}
}

func ptchanContextGoldenFixtures() ([]string, error) {
	fixtures, err := filepath.Glob(filepath.Join("testdata", "ptchan_context", "*.json"))
	if err != nil {
		return nil, err
	}
	if os.Getenv("MARTIE_INCLUDE_LOCAL_GOLDEN") != "1" {
		return fixtures, nil
	}
	localFixtures, err := filepath.Glob(filepath.Join("testdata", "ptchan_context", "local", "*.json"))
	if err != nil {
		return nil, err
	}
	return append(fixtures, localFixtures...), nil
}

func readPtchanContextGoldenMeta(t *testing.T, path string) ptchanContextGoldenMeta {
	t.Helper()
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return ptchanContextGoldenMeta{}
	}
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}
	var meta ptchanContextGoldenMeta
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	return meta
}

func firstStringDiff(want, got string) string {
	wantLines := strings.Split(want, "\n")
	gotLines := strings.Split(got, "\n")
	for i := 0; i < len(wantLines) || i < len(gotLines); i++ {
		var wantLine, gotLine string
		if i < len(wantLines) {
			wantLine = wantLines[i]
		}
		if i < len(gotLines) {
			gotLine = gotLines[i]
		}
		if wantLine != gotLine {
			return "first differing line " + strconv.Itoa(i+1) + "\nwant: " + wantLine + "\n got: " + gotLine
		}
	}
	return "contents differ"
}
