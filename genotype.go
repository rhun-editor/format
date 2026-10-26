package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	genotypeVersion = regexp.MustCompile(`^gt ([0-9]+)\.([0-9]+)\.([0-9]+)(?:[-+][0-9A-Za-z.+-]+)?$`)
	genotypeANSI    = regexp.MustCompile("\x1b\\[[0-?]*[ -/]*[@-~]")
)

func supportedGenotype(version []byte) bool {
	parts := genotypeVersion.FindSubmatch(bytes.TrimSpace(version))
	if parts == nil {
		return false
	}
	var numbers [3]int
	for i := range numbers {
		n, err := strconv.Atoi(string(parts[i+1]))
		if err != nil {
			return false
		}
		numbers[i] = n
	}
	return numbers[0] > 0 || numbers[1] >= 18
}

// gt fmt writes every .type file under its working directory. Give it only the current buffer
// in the extension's private state folder; the project and its saved files are never inputs.
func runGenotype(program, state string, text []byte, timeout time.Duration) ([]byte, error) {
	if state == "" {
		return nil, fmt.Errorf("gt: the extension state folder is unavailable")
	}
	program, err := filepath.Abs(program)
	if err != nil {
		return nil, fmt.Errorf("gt: resolve executable: %w", err)
	}
	folder, err := os.MkdirTemp(state, "genotype-")
	if err != nil {
		return nil, fmt.Errorf("gt: create temporary folder: %w", err)
	}
	defer os.RemoveAll(folder)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Other programs also use the name gt. Identify the CLI before giving it buffer contents.
	var stderr bytes.Buffer
	probe := exec.CommandContext(ctx, program, "--version")
	probe.Dir, probe.Stderr = folder, &stderr
	probe.WaitDelay = 100 * time.Millisecond
	version, err := probe.Output()
	if err != nil {
		return nil, genotypeError(ctx, err, stderr.Bytes())
	}
	if !supportedGenotype(version) {
		return nil, fmt.Errorf("gt: Genotype CLI 0.18.0 or later is required")
	}
	input := filepath.Join(folder, "input.type")
	if err := os.WriteFile(input, text, 0600); err != nil {
		return nil, fmt.Errorf("gt: write buffer: %w", err)
	}
	stderr.Reset()
	cmd := exec.CommandContext(ctx, program, "fmt")
	cmd.Dir, cmd.Stderr = folder, &stderr
	cmd.WaitDelay = 100 * time.Millisecond
	if err := cmd.Run(); err != nil {
		return nil, genotypeError(ctx, err, stderr.Bytes())
	}
	out, err := os.ReadFile(input)
	if err != nil {
		return nil, fmt.Errorf("gt: read formatted text: %w", err)
	}
	return out, nil
}

func genotypeError(ctx context.Context, err error, stderr []byte) error {
	if ctx.Err() != nil {
		return fmt.Errorf("gt: formatting timed out")
	}
	message := strings.Join(strings.Fields(genotypeANSI.ReplaceAllString(string(stderr), "")), " ")
	if message == "" {
		message = err.Error()
	}
	return fmt.Errorf("gt: %s", message)
}
