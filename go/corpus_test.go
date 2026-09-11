package config

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The corpus is the cross-language instrument: abstraction-config/testdata/scenarios holds
// a scenario and the transcript an observer should see, and the Go driver here
// and abstraction-config/python/driver.py must each print those bytes. Two languages that
// match one recorded file match each other, and the file also pins the answer,
// which two transcripts diffed against each other never do.

func TestTheCorpusTranscriptsAreWhatIsRecorded(t *testing.T) {
	dir := scenarios(t)
	driver := build(t)
	declared := capabilities(t, driver)
	names, err := filepath.Glob(filepath.Join(dir, "*.txt"))
	if err != nil || len(names) == 0 {
		t.Fatalf("no scenarios beside %s: %v", dir, err)
	}
	for _, scenario := range names {
		name := strings.TrimSuffix(filepath.Base(scenario), ".txt")
		t.Run(name, func(t *testing.T) {
			body, err := os.ReadFile(scenario)
			if err != nil {
				t.Fatal(err)
			}
			for _, need := range requires(string(body)) {
				if !declared[need] {
					// Out of reach is never a pass, and go test has no rung
					// between pass and fail, so the line goes to stderr where
					// the runner streams it rather than into a captured log
					// nobody reads on a green run.
					fmt.Fprintf(os.Stderr, "UNREACHABLE  config corpus %s — this driver does not declare %q on %s\n",
						name, need, runtime.GOOS)
					t.Skip("unreachable, and said so on stderr")
				}
			}
			want, err := os.ReadFile(filepath.Join(dir, name+".expected"))
			if err != nil {
				t.Fatalf("no recorded transcript: %v", err)
			}
			out, err := exec.Command(driver, t.TempDir(), scenario).Output()
			if err != nil {
				t.Fatalf("the driver broke: %v", err)
			}
			if string(out) != string(want) {
				t.Fatalf("transcript differs from the recorded one\n got:\n%s\nwant:\n%s", out, want)
			}
		})
	}
}

// A rule with no expectation citing it is a rule this corpus does not reach,
// and rules.tsv is where that is admitted. A tag cited by a scenario and named
// by no rule is the other half of the same defect.
func TestEveryTagCitedIsARuleAndEveryCoveredRuleIsCited(t *testing.T) {
	dir := scenarios(t)
	ledger, err := os.ReadFile(filepath.Join(dir, "rules.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	known := map[string]string{}
	for _, line := range strings.Split(string(ledger), "\n") {
		if strings.HasPrefix(line, ">") || strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 3 {
			t.Fatalf("rules.tsv wants three tab-separated fields: %q", line)
		}
		known[f[0]] = f[2]
	}
	cited := map[string]bool{}
	names, _ := filepath.Glob(filepath.Join(dir, "*.txt"))
	for _, scenario := range names {
		body, err := os.ReadFile(scenario)
		if err != nil {
			t.Fatal(err)
		}
		for _, tag := range tags(string(body)) {
			if _, ok := known[tag]; !ok {
				t.Errorf("%s cites [%s], which rules.tsv does not name", filepath.Base(scenario), tag)
			}
			cited[tag] = true
		}
	}
	for tag, holds := range known {
		reached := !strings.HasPrefix(holds, "untestable:") && !strings.HasPrefix(holds, "not built:")
		if reached && !cited[tag] {
			t.Errorf("rules.tsv says %s is covered by %q, and no expectation cites it", tag, holds)
		}
		if !reached && cited[tag] {
			t.Errorf("rules.tsv calls %s unreached and a scenario cites it anyway", tag)
		}
	}
}

func scenarios(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "testdata", "scenarios"))
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func build(t *testing.T) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "configdriver.exe")
	cmd := exec.Command("go", "build", "-o", out, "./cmd/configdriver")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the driver will not build, so the corpus judged nothing: %v\n%s", err, b)
	}
	return out
}

func capabilities(t *testing.T, driver string) map[string]bool {
	t.Helper()
	out, err := exec.Command(driver, "--capabilities").Output()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, tok := range strings.Fields(string(out)) {
		got[tok] = true
	}
	if len(got) == 0 {
		t.Fatal("the driver declared nothing, so nothing here can be judged")
	}
	return got
}

func requires(body string) []string {
	for _, line := range strings.Split(body, "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "# requires:"); ok {
			return strings.Fields(rest)
		}
	}
	return nil
}

func tags(body string) []string {
	var out []string
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "# expect ") {
			continue
		}
		for _, word := range strings.Fields(line) {
			if strings.HasPrefix(word, "[") && strings.HasSuffix(word, "]") {
				out = append(out, strings.Trim(word, "[]"))
			}
		}
	}
	return out
}
