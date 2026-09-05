package node

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

const sentinel = "sentinel-producer-password-7f3a"

func producerConfig() Config {
	cfg := DefaultNodeConfig
	cfg.Producer = &ProducerConfig{
		Address:     "z1qqjnwjjpnue8xmmpanz6csze6tcmtzzdtfsww7",
		Index:       3,
		KeyFilePath: "/keys/producer",
		Password:    sentinel,
	}
	return cfg
}

// allVerbs covers the string, numeric, float, rune, pointer and boolean
// verbs, with the flags fmt treats specially for structs and strings.
var allVerbs = []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%X", "%d", "%p", "%e", "%f", "%g", "%c", "%t", "%b", "%o", "%U", "%T"}

// Every generic rendering of the configuration, of the producer section and
// of the field itself, directly or through pointers and enclosing values,
// must hide the password under every verb; only an explicit conversion
// yields the value.
func TestProducerPasswordIsRedactedWhenFormatted(t *testing.T) {
	cfg := producerConfig()
	type wrapsProducer struct{ P *ProducerConfig }
	type wrapsConfig struct{ C *Config }
	type wrapsConfigValue struct{ C Config }
	subjects := map[string]interface{}{
		"secret":               cfg.Producer.Password,
		"secret ptr":           &cfg.Producer.Password,
		"producer":             *cfg.Producer,
		"producer ptr":         cfg.Producer,
		"config":               cfg,
		"config ptr":           &cfg,
		"wrapped producer":     wrapsProducer{cfg.Producer},
		"wrapped config":       wrapsConfig{&cfg},
		"wrapped config ptr":   &wrapsConfig{&cfg},
		"wrapped config value": &wrapsConfigValue{cfg},
		"slice of producers":   []*ProducerConfig{cfg.Producer},
		"map of configs":       map[string]Config{"a": cfg},
	}
	for name, subject := range subjects {
		for _, verb := range allVerbs {
			// fmt handles %p before consulting any method of the operand and,
			// for a non-pointer, prints a diagnostic with the raw value; no
			// type can intercept that, so %p is only checked on pointers,
			// where it prints an address.
			if verb == "%p" && !strings.Contains(name, "ptr") {
				continue
			}
			out := fmt.Sprintf(verb, subject)
			if strings.Contains(out, sentinel) {
				t.Errorf("%s with %s reveals the password: %s", name, verb, out)
			}
		}
	}
	for _, verb := range []string{"%v", "%+v", "%#v", "%s"} {
		if out := fmt.Sprintf(verb, *cfg.Producer); !strings.Contains(out, "/keys/producer") {
			t.Errorf("producer %s lost non-secret fields: %s", verb, out)
		}
		if out := fmt.Sprintf(verb, cfg); !strings.Contains(out, "znn-node") {
			t.Errorf("config %s lost non-secret fields: %s", verb, out)
		}
	}
	if out := fmt.Sprintf("%v", cfg.Producer.Password); out != "<redacted>" {
		t.Errorf("secret renders as %q", out)
	}
	if out := fmt.Sprintf("%v", Secret("")); out != "" {
		t.Errorf("empty secret renders as %q", out)
	}
	if got := string(cfg.Producer.Password); got != sentinel {
		t.Errorf("conversion yields %q", got)
	}
}

// JSON output redacts the password; JSON input still carries it, so
// config.json keeps working.
func TestProducerPasswordJSON(t *testing.T) {
	cfg := producerConfig()
	for name, marshal := range map[string]func(interface{}) ([]byte, error){
		"Marshal":       json.Marshal,
		"MarshalIndent": func(v interface{}) ([]byte, error) { return json.MarshalIndent(v, "", "  ") },
	} {
		out, err := marshal(cfg)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if strings.Contains(string(out), sentinel) {
			t.Errorf("%s reveals the password: %s", name, out)
		}
		if !strings.Contains(string(out), `"KeyFilePath": "/keys/producer"`) && !strings.Contains(string(out), `"KeyFilePath":"/keys/producer"`) {
			t.Errorf("%s lost non-secret fields: %s", name, out)
		}
	}

	var parsed Config
	if err := json.Unmarshal([]byte(`{"Producer":{"Address":"a","KeyFilePath":"k","Password":"`+sentinel+`"}}`), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Producer == nil || string(parsed.Producer.Password) != sentinel {
		t.Fatalf("password did not round-trip from JSON: %+v", parsed.Producer)
	}

	// An empty password stays empty rather than becoming a marker.
	empty := Config{Producer: &ProducerConfig{}}
	out, _ := json.Marshal(empty)
	if !strings.Contains(string(out), `"Password":""`) {
		t.Errorf("empty password rendered as %s", out)
	}
}
