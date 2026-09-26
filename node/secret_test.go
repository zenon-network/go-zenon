package node

import (
	"encoding/json"
	"errors"
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
// verbs, with the flags fmt treats specially for structs and strings, and
// width, precision and alternate-form flags on the common verbs. %w is
// absent: like %p on a non-pointer value it is rejected by fmt before any
// method is consulted, see TestSecretVerbsFmtRejectsBeforeMethods.
var allVerbs = []string{
	"%v", "%+v", "%#v", "%s", "%q", "%x", "%X", "%d", "%p", "%e", "%f", "%g", "%c", "%t", "%b", "%o", "%U", "%T",
	"%12s", "%-12s", "%.3s", "%12v", "%-12v", "%+q", "%#q", "% x", "%#x", "%08d", "%6.2f",
}

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
		"slice of secrets":     []Secret{cfg.Producer.Password},
		"map keyed by secret":  map[Secret]int{cfg.Producer.Password: 1},
		"interface slice":      []interface{}{cfg.Producer.Password, cfg.Producer},
	}
	for name, subject := range subjects {
		for _, out := range []string{fmt.Sprint(subject), fmt.Sprintln(subject), fmt.Errorf("%v", subject).Error()} {
			if strings.Contains(out, sentinel) {
				t.Errorf("%s reveals the password: %s", name, out)
			}
		}
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
	if out := fmt.Sprintf("%v", cfg.Producer.Password); out != redactedMarker {
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

// Decoding a Secret is encoding/json's own decoding of a string kind, so
// every input has the same outcome as for a plain string field: a null
// leaves the value unchanged, a duplicate key takes the last value, a
// non-string value is a type error naming the field while the sibling
// fields still decode, and malformed input is a syntax error at the same
// offset. A nil pointer field is left nil by null and allocated by a
// string.
func TestSecretDecodesLikeAPlainString(t *testing.T) {
	type plain struct {
		Password string
		Tail     int
	}
	type secretly struct {
		Password Secret
		Tail     int
	}
	for _, in := range []string{
		`{"Password":null,"Tail":7}`, `{"Tail":7}`, `null`,
		`{"Password":"first","Password":"second","Tail":7}`,
		`{"Password":12,"Tail":7}`, `{"Password":true,"Tail":7}`, `{"Password":{},"Tail":7}`, `{"Password":[],"Tail":7}`,
		`{"Password":"unterminated,"Tail":7}`, `{"Password":nul,"Tail":7}`, `{"Password":"a","Tail":7`,
	} {
		before, after := plain{sentinel, 1}, secretly{sentinel, 1}
		errPlain := json.Unmarshal([]byte(in), &before)
		errSecret := json.Unmarshal([]byte(in), &after)
		if string(after.Password) != before.Password || after.Tail != before.Tail {
			t.Errorf("%s: plain string field decodes to %+v, Secret to %+v", in, before, after)
		}
		if (errPlain == nil) != (errSecret == nil) {
			t.Errorf("%s: plain string error %v, Secret error %v", in, errPlain, errSecret)
		}
		var typePlain, typeSecret *json.UnmarshalTypeError
		if errors.As(errPlain, &typePlain) != errors.As(errSecret, &typeSecret) {
			t.Errorf("%s: plain string error %v, Secret error %v", in, errPlain, errSecret)
		} else if typePlain != nil && (typeSecret.Field != typePlain.Field || typeSecret.Value != typePlain.Value || typeSecret.Offset != typePlain.Offset || typeSecret.Field != "Password") {
			t.Errorf("%s: plain string type error %+v, Secret type error %+v", in, typePlain, typeSecret)
		}
		var syntaxPlain, syntaxSecret *json.SyntaxError
		if errors.As(errPlain, &syntaxPlain) != errors.As(errSecret, &syntaxSecret) {
			t.Errorf("%s: plain string error %v, Secret error %v", in, errPlain, errSecret)
		} else if syntaxPlain != nil && syntaxSecret.Offset != syntaxPlain.Offset {
			t.Errorf("%s: plain string syntax error at %d, Secret at %d", in, syntaxPlain.Offset, syntaxSecret.Offset)
		}
	}

	secret := Secret(sentinel)
	if err := json.Unmarshal([]byte(`null`), &secret); err != nil {
		t.Fatal(err)
	}
	if string(secret) != sentinel {
		t.Errorf("null changed the value to %q", secret)
	}

	type pointers struct{ P *Secret }
	var viaPointer pointers
	if err := json.Unmarshal([]byte(`{"P":null}`), &viaPointer); err != nil || viaPointer.P != nil {
		t.Errorf("null into a nil pointer field: err %v, field %v", err, viaPointer.P)
	}
	if err := json.Unmarshal([]byte(`{"P":"`+sentinel+`"}`), &viaPointer); err != nil || viaPointer.P == nil || string(*viaPointer.P) != sentinel {
		t.Errorf("string into a nil pointer field: err %v, field %v", err, viaPointer.P)
	}
	if err := json.Unmarshal([]byte(`{"P":null}`), &viaPointer); err != nil || viaPointer.P != nil {
		t.Errorf("null into a set pointer field: err %v, field %v", err, viaPointer.P)
	}

	cfg := producerConfig()
	if err := json.Unmarshal([]byte(`{"Producer":{"Address":"other","Password":null}}`), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Producer.Address != "other" {
		t.Errorf("sibling field not decoded: %+v", cfg.Producer)
	}
	if string(cfg.Producer.Password) != sentinel {
		t.Errorf("null cleared the configured password: %+v", cfg.Producer)
	}
	cfg.Producer = nil
	if err := json.Unmarshal([]byte(`{"Producer":{"Address":"other","Password":null}}`), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Producer == nil || cfg.Producer.Address != "other" || cfg.Producer.Password != "" {
		t.Errorf("null under a producer section that was nil: %+v", cfg.Producer)
	}
}

// fmt rejects %p on a non-pointer value and %w on a value that is not an
// error before it consults any method, and prints its diagnostic with the
// raw value when the operand is a Secret or a producer section holding
// one; go vet's printf check reports both misuses. A Config holds the
// section behind a pointer, which the diagnostic path prints as an
// address, so the value stays hidden there. Pinned so that the documented
// exceptions stay exactly these, and so that a Go release that changes
// this is noticed.
func TestSecretVerbsFmtRejectsBeforeMethods(t *testing.T) {
	cfg := producerConfig()
	subjects := map[string]struct {
		value    interface{}
		revealed bool
	}{
		"secret":       {cfg.Producer.Password, true},
		"producer":     {*cfg.Producer, true},
		"producer ptr": {cfg.Producer, true},
		"config":       {cfg, false},
		"config ptr":   {&cfg, false},
	}
	for name, subject := range subjects {
		for _, verb := range []string{"%p", "%w"} {
			if verb == "%p" && strings.Contains(name, "ptr") {
				continue // prints an address
			}
			out := fmt.Sprintf(verb, subject.value)
			if !strings.HasPrefix(out, "%!"+verb[1:]+"(") {
				t.Errorf("%s with %s is no longer fmt's diagnostic: %s", name, verb, out)
			}
			if strings.Contains(out, sentinel) != subject.revealed {
				t.Errorf("%s with %s: revealed=%v, want %v; adjust the exception documented on Secret.Format: %s", name, verb, !subject.revealed, subject.revealed, out)
			}
		}
	}
	wrap := "%w" // a variable so go vet does not reject the misuse the test pins
	if out := fmt.Errorf(wrap, cfg.Producer.Password).Error(); !strings.HasPrefix(out, "%!w(") || !strings.Contains(out, sentinel) {
		t.Errorf("Errorf %%w on a non-error is no longer fmt's diagnostic with the value: %s", out)
	}
}

// encoding/json uses map keys of string kind directly, so a Secret used as
// a key is written as is; the guarantee covers values and fields.
func TestSecretAsJSONMapKeyIsWrittenAsIs(t *testing.T) {
	out, err := json.Marshal(map[Secret]int{sentinel: 1})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"`+sentinel+`":1}` {
		t.Errorf("map key rendered as %s; the limit documented on Secret can be narrowed", out)
	}
	out, err = json.Marshal(map[string]Secret{"k": sentinel})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"k":"`+redactedMarker+`"}` {
		t.Errorf("map value rendered as %s", out)
	}
}

// Passwords that JSON escapes, or that are not ASCII, decode to the exact
// bytes and are still hidden by every rendering.
func TestSecretEscapedAndNonASCII(t *testing.T) {
	for _, password := range []string{
		"pa\"ss\\word",
		"tab\there\nnewline",
		"<b>&amp;</b>",
		"wörd-passwørd-пароль-密码",
		"emoji-\U0001F511-key",
		"line\u2028sep\u2029",
		"nul\x00byte",
	} {
		raw, err := json.Marshal(map[string]interface{}{"Producer": map[string]interface{}{"Password": password}})
		if err != nil {
			t.Fatal(err)
		}
		var cfg Config
		if err := json.Unmarshal(raw, &cfg); err != nil {
			t.Fatalf("%q: %v", password, err)
		}
		if got := string(cfg.Producer.Password); got != password {
			t.Errorf("decoded %q as %q", password, got)
		}

		out, err := json.Marshal(cfg)
		if err != nil {
			t.Fatal(err)
		}
		var decoded Config
		if err := json.Unmarshal(out, &decoded); err != nil {
			t.Fatal(err)
		}
		if string(decoded.Producer.Password) != redactedMarker {
			t.Errorf("%q: encoded form decodes to %q, want the marker", password, decoded.Producer.Password)
		}
		encodedMarker := strings.Trim(string(mustMarshal(t, redactedMarker)), `"`)
		for name, rendering := range map[string]string{
			"json":     strings.ReplaceAll(string(out), encodedMarker, redactedMarker),
			"%v":       fmt.Sprintf("%v", cfg),
			"%+v":      fmt.Sprintf("%+v", &cfg),
			"%#v":      fmt.Sprintf("%#v", cfg.Producer),
			"%q":       fmt.Sprintf("%q", cfg.Producer.Password),
			"%x":       fmt.Sprintf("%x", cfg.Producer.Password),
			"producer": fmt.Sprintf("%v", *cfg.Producer),
		} {
			if !strings.Contains(rendering, redactedMarker) {
				t.Errorf("%q: %s rendering lacks the marker: %s", password, name, rendering)
			}
			for _, fragment := range []string{password, strings.Trim(string(mustMarshal(t, password)), `"`), fmt.Sprintf("%x", password), fmt.Sprintf("%q", password)} {
				if fragment != "" && strings.Contains(rendering, fragment) {
					t.Errorf("%q: %s rendering reveals the password: %s", password, name, rendering)
				}
			}
		}
	}
}

// The encoded form is diagnostic output, not a persistence format: decoding
// it yields the literal marker, never the value.
func TestSecretEncodedFormDecodesToMarker(t *testing.T) {
	cfg := producerConfig()
	out, err := json.MarshalIndent(cfg, "", "    ")
	if err != nil {
		t.Fatal(err)
	}
	var decoded Config
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatal(err)
	}
	if got := string(decoded.Producer.Password); got != redactedMarker {
		t.Errorf("decoded password is %q, want %q", got, redactedMarker)
	}
	if decoded.Producer.KeyFilePath != cfg.Producer.KeyFilePath || decoded.Producer.Index != cfg.Producer.Index {
		t.Errorf("non-secret fields did not round-trip: %+v", decoded.Producer)
	}
	// A string variable needs a conversion in both directions.
	value := "from-a-variable"
	cfg.Producer.Password = Secret(value)
	if string(cfg.Producer.Password) != value {
		t.Errorf("conversion round trip yields %q", cfg.Producer.Password)
	}
}

func mustMarshal(t *testing.T, v interface{}) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
