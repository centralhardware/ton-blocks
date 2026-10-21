package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// TVM assembly for ton.code, from the official fift disassembler. The image
// carries fift (extracted from the release AppImage: no FUSE in a container)
// and its lib/ under FIFT_DIR; without it the step logs once and is skipped.
const asmBatch = 100 // codes disassembled per round

var fiftDir = envOr("FIFT_DIR", "/opt/fift")

var fiftWarned bool

func fiftAvailable() bool {
	_, err := os.Stat(filepath.Join(fiftDir, "app", "usr", "bin", "fift"))
	if err != nil && !fiftWarned {
		fiftWarned = true
		log.Printf("disassembly off: %v", err)
	}
	return err == nil
}

func disassemble(bocB64 string) (string, error) {
	boc, err := base64.StdEncoding.DecodeString(bocB64)
	if err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp("", "asm")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	bocPath := filepath.Join(tmp, "code.boc")
	script := filepath.Join(tmp, "disasm.fif")
	if err := os.WriteFile(bocPath, boc, 0o600); err != nil {
		return "", err
	}
	if err := os.WriteFile(script, []byte(fmt.Sprintf("\"Disasm.fif\" include\n%q file>B B>boc <s disasm\n", bocPath)), 0o600); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(fiftDir, "app", "usr", "bin", "fift"), "-I", filepath.Join(fiftDir, "lib"), "-s", script)
	cmd.Env = append(os.Environ(), "LD_LIBRARY_PATH="+filepath.Join(fiftDir, "app", "usr", "lib"))
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String() + " " + out.String())
		return "", fmt.Errorf("%v: %.300s", err, msg)
	}
	return out.String(), nil
}

// disassembleNewCode fills asm for codes stored without it, one INSERT per round.
func disassembleNewCode() error {
	if !fiftAvailable() {
		return nil
	}
	out, err := post(fmt.Sprintf("SELECT code_hash, code_boc, size, example FROM %s.code FINAL "+
		"WHERE asm = '' AND asm_error = '' LIMIT %d FORMAT JSONEachRow", database, asmBatch), nil)
	if err != nil {
		return err
	}
	var done []code
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		var c code
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			return err
		}
		if c.Asm, err = disassemble(c.CodeBOC); err != nil {
			c.AsmError = err.Error()
		}
		done = append(done, c)
	}
	if len(done) > 0 {
		log.Printf("disassembled %d codes", len(done))
	}
	return insert("code", done)
}
