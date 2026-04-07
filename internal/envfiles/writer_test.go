package envfiles

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/morapet/kdc/internal/registry"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestWrite_ConfigMap(t *testing.T) {
	reg := registry.New()
	reg.AddConfigMap(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "app-config"},
		Data: map[string]string{
			"LOG_LEVEL":   "info",
			"MAX_CONN":    "100",
			"WITH_SPACES": "hello world",
		},
	})

	dir := t.TempDir()
	if err := Write(reg, dir); err != nil {
		t.Fatalf("Write error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "app-config.env"))
	if err != nil {
		t.Fatalf("read env file: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "LOG_LEVEL=info") {
		t.Errorf("missing LOG_LEVEL=info in:\n%s", content)
	}
	if !strings.Contains(content, "MAX_CONN=100") {
		t.Errorf("missing MAX_CONN=100 in:\n%s", content)
	}
	// Value with spaces should be quoted.
	if !strings.Contains(content, `WITH_SPACES="hello world"`) {
		t.Errorf("expected quoted WITH_SPACES in:\n%s", content)
	}
}

func TestWrite_Secret(t *testing.T) {
	reg := registry.New()
	reg.AddSecret(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "db-secret"},
		Data: map[string][]byte{
			"password": []byte("s3cr3t"),
			"username": []byte("dbuser"),
		},
	})

	dir := t.TempDir()
	if err := Write(reg, dir); err != nil {
		t.Fatalf("Write error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "db-secret.env"))
	if err != nil {
		t.Fatalf("read secret env file: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "password=s3cr3t") {
		t.Errorf("missing password=s3cr3t in:\n%s", content)
	}
}

func TestQuoteEnvValue_DollarEscaping(t *testing.T) {
	// A value containing $ should be quoted and $ escaped to $$
	// so Docker Compose doesn't treat it as a variable reference.
	got := quoteEnvValue("password$123")
	want := `"password$$123"`
	if got != want {
		t.Errorf("quoteEnvValue(%q) = %q, want %q", "password$123", got, want)
	}

	// Multiple $ signs
	got = quoteEnvValue("a$b$c")
	want = `"a$$b$$c"`
	if got != want {
		t.Errorf("quoteEnvValue(%q) = %q, want %q", "a$b$c", got, want)
	}
}

func TestWrite_Deterministic(t *testing.T) {
	reg := registry.New()
	reg.AddConfigMap(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "cfg"},
		Data:       map[string]string{"Z": "z", "A": "a", "M": "m"},
	})

	dir1 := t.TempDir()
	dir2 := t.TempDir()
	_ = Write(reg, dir1)
	_ = Write(reg, dir2)

	b1, _ := os.ReadFile(filepath.Join(dir1, "cfg.env"))
	b2, _ := os.ReadFile(filepath.Join(dir2, "cfg.env"))
	if string(b1) != string(b2) {
		t.Error("output not deterministic")
	}
	// Keys should be sorted.
	if !strings.Contains(string(b1), "A=a\nM=m\nZ=z") {
		t.Errorf("keys not sorted in:\n%s", string(b1))
	}
}
