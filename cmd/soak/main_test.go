package main

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRunSoak_BasicShortDuration(t *testing.T) {
	cfg := soakConfig{
		duration:    120 * time.Millisecond,
		concurrency: 4,
		rps:         200,
	}
	var out bytes.Buffer
	if err := runSoak(cfg, &out); err != nil {
		t.Fatalf("runSoak failed: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "duration=120ms") {
		t.Fatalf("unexpected output duration: %q", got)
	}
	if !strings.Contains(got, "rps_target=200") {
		t.Fatalf("unexpected rps target: %q", got)
	}
	total := extractUintMetric(t, got, "total")
	if total == 0 {
		t.Fatalf("expected total requests > 0, got output: %q", got)
	}
}

func TestRunSoak_UnlimitedRPS(t *testing.T) {
	cfg := soakConfig{
		duration:    80 * time.Millisecond,
		concurrency: 2,
		rps:         0,
	}
	var out bytes.Buffer
	if err := runSoak(cfg, &out); err != nil {
		t.Fatalf("runSoak failed: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "rps_target=0") {
		t.Fatalf("unexpected output: %q", got)
	}
	qps := extractFloatMetric(t, got, "qps")
	if qps <= 0 {
		t.Fatalf("expected qps > 0, got output: %q", got)
	}
}

func TestFastRand_Intn(t *testing.T) {
	r1 := newFastRand(42)
	r2 := newFastRand(42)
	for i := 0; i < 64; i++ {
		if a, b := r1.next(), r2.next(); a != b {
			t.Fatalf("expected deterministic sequence for same seed: %d != %d", a, b)
		}
	}

	r := newFastRand(1)
	for i := 0; i < 1024; i++ {
		v := r.Intn(7)
		if v < 0 || v >= 7 {
			t.Fatalf("Intn out of range: %d", v)
		}
	}
	if got := r.Intn(0); got != 0 {
		t.Fatalf("expected Intn(0)=0, got %d", got)
	}
	if got := r.Intn(-1); got != 0 {
		t.Fatalf("expected Intn(-1)=0, got %d", got)
	}
}

func TestRandSeed_NonZero(t *testing.T) {
	seed := randSeed()
	if seed == 0 {
		t.Fatalf("expected non-zero seed")
	}
	r := newFastRand(0)
	if r.state == 0 {
		t.Fatalf("expected non-zero state for zero-seed constructor")
	}
}

func TestParseSoakConfig(t *testing.T) {
	cfg, err := parseSoakConfig([]string{"-duration=250ms", "-concurrency=3", "-rps=0"})
	if err != nil {
		t.Fatalf("parseSoakConfig failed: %v", err)
	}
	if cfg.duration != 250*time.Millisecond || cfg.concurrency != 3 || cfg.rps != 0 {
		t.Fatalf("unexpected parsed config: %+v", cfg)
	}
}

func TestRunCLI_ParseError(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runCLI([]string{"-duration=bad"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("expected exit code 2, got %d", code)
	}
	if !strings.Contains(stderr.String(), "soak failed") {
		t.Fatalf("expected parse error output, got %q", stderr.String())
	}
}

func TestRunCLI_Success(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runCLI([]string{"-duration=20ms", "-concurrency=1", "-rps=0"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr=%q)", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "qps=") {
		t.Fatalf("expected benchmark output, got %q", stdout.String())
	}
}

func extractUintMetric(t *testing.T, text, key string) uint64 {
	t.Helper()
	re := regexp.MustCompile(key + `=([0-9]+)`)
	m := re.FindStringSubmatch(text)
	if len(m) != 2 {
		t.Fatalf("missing metric %s in %q", key, text)
	}
	v, err := strconv.ParseUint(m[1], 10, 64)
	if err != nil {
		t.Fatalf("parse metric %s failed: %v", key, err)
	}
	return v
}

func extractFloatMetric(t *testing.T, text, key string) float64 {
	t.Helper()
	re := regexp.MustCompile(key + `=([0-9]+(?:\.[0-9]+)?)`)
	m := re.FindStringSubmatch(text)
	if len(m) != 2 {
		t.Fatalf("missing metric %s in %q", key, text)
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		t.Fatalf("parse metric %s failed: %v", key, err)
	}
	return v
}
