package phoneops

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeployRollbackPreservesConfig(t *testing.T) {
	for _, mode := range []string{"success", "launch-failure", "health-failure"} {
		t.Run(mode, func(t *testing.T) {
			fail := mode != "success"
			dir := t.TempDir()
			stage := filepath.Join(dir, "stage")
			os.Mkdir(stage, 0700)
			bin := filepath.Join(dir, "bin")
			os.Mkdir(bin, 0700)
			os.WriteFile(filepath.Join(dir, "omnihub-server"), []byte("old"), 0700)
			os.WriteFile(filepath.Join(dir, "config.json"), []byte("preserved"), 0600)
			launcher := "#!/bin/sh\nexit 0\n"
			os.WriteFile(filepath.Join(dir, "omnihub.sh"), []byte(launcher), 0700)
			os.WriteFile(filepath.Join(stage, "omnihub-server"), []byte("new"), 0700)
			if mode == "launch-failure" {
				launcher = "#!/bin/sh\nexit 1\n"
			}
			os.WriteFile(filepath.Join(stage, "omnihub.sh"), []byte(launcher), 0700)
			os.WriteFile(filepath.Join(bin, "sleep"), []byte("#!/bin/sh\nexit 0\n"), 0700)
			http := "200"
			if mode == "health-failure" {
				http = "500"
			}
			os.WriteFile(filepath.Join(bin, "curl"), []byte("#!/bin/sh\necho "+http+"\n"), 0700)
			cmd := exec.CommandContext(context.Background(), "sh", "-c", deployScript(dir, stage, 8998))
			cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"))
			out, err := cmd.CombinedOutput()
			if (err != nil) != fail {
				t.Fatalf("result %v %s", err, out)
			}
			got, _ := os.ReadFile(filepath.Join(dir, "omnihub-server"))
			expected := "new"
			if fail {
				expected = "old"
			}
			if string(got) != expected {
				t.Fatalf("program %s", got)
			}
			cfg, _ := os.ReadFile(filepath.Join(dir, "config.json"))
			if string(cfg) != "preserved" {
				t.Fatal("config changed")
			}
			result, _ := os.ReadFile(filepath.Join(stage, "result"))
			if !fail && strings.TrimSpace(string(result)) != "ok" {
				t.Fatal("no result")
			}
		})
	}
}
