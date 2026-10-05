package db

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/statehygiene"
	"gopkg.in/yaml.v3"
)

func TestMarshalStateForWriteScalarRoundTrips(t *testing.T) {
	texts := []string{
		"ordinary text", "\n---\nBlockers: 2\n- item", "\n\nfirst\nsecond\n",
		"  foo\n  bar\n", "\tfoo\n\tbar\n", "\n\n", "\n", "",
		"body: |4-\n  content\n", "\nbody: |4-\n  content\n",
		"λ: >4+\n雪\n", "- key: |4-\n  text\n", "line\n\nend\n\n",
		"\n\xff\xfe\n",
	}
	for i, text := range texts {
		for _, shape := range []string{"value", "mapping", "sequence", "sequence-mapping", "nested-sequence"} {
			t.Run(fmt.Sprintf("%d/%s", i, shape), func(t *testing.T) {
				t.Parallel()
				var value any = text
				switch shape {
				case "mapping":
					value = map[string]any{"reason": text}
				case "sequence":
					value = []any{text, "sibling"}
				case "sequence-mapping":
					value = []any{map[string]any{"reason": text}, map[string]any{"reason": "sibling"}}
				case "nested-sequence":
					value = []any{[]any{map[string]any{"reason": text}}}
				}
				state := &models.State{Extra: map[string]any{"value": value}}
				data, err := marshalStateForWrite(state)
				if err != nil {
					t.Fatalf("marshal: %v", err)
				}
				var decoded models.State
				if err := yaml.Unmarshal(data, &decoded); err != nil {
					t.Fatalf("independent decode: %v\n%s", err, data)
				}
				if got := decoded.Extra["value"]; !reflect.DeepEqual(got, value) {
					t.Fatalf("scalar text changed: got %#v, want %#v", got, value)
				}
			})
		}
	}
}

func TestMarshalStateForWritePreservesValidBytes(t *testing.T) {
	t.Parallel()
	state := &models.State{Extra: map[string]any{
		"ordinary": "foo\nbar\n", "content": "body: |4-\n  text\n", "number": 42, "flag": true,
	}}
	want, err := yaml.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	var decoded models.State
	if err := yaml.Unmarshal(want, &decoded); err != nil {
		t.Fatalf("fixture is not valid emitter output: %v", err)
	}
	if !reflect.DeepEqual(decoded.Extra, state.Extra) {
		t.Fatalf("fixture does not preserve values: %#v", decoded.Extra)
	}
	got, err := marshalStateForWrite(state)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("valid emitter output changed\ngot:\n%s\nwant:\n%s", got, want)
	}
}

type aliasMarshaler struct{}

func (aliasMarshaler) MarshalYAML() (any, error) {
	return &yaml.Node{Kind: yaml.AliasNode, Value: "undefined"}, nil
}

type pointerAliasMarshaler struct{}

func (*pointerAliasMarshaler) MarshalYAML() (any, error) {
	return &yaml.Node{Kind: yaml.AliasNode, Value: "undefined"}, nil
}

func TestMarshalStateForWriteRefusesMalformedCustomValues(t *testing.T) {
	values := []any{
		&yaml.Node{Kind: yaml.AliasNode, Value: "undefined"},
		aliasMarshaler{}, &aliasMarshaler{}, &pointerAliasMarshaler{},
		[]any{map[string]any{"nested": &pointerAliasMarshaler{}}},
		map[any]any{int(1): "one", int64(1): "duplicate"},
	}
	for i, value := range values {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			t.Parallel()
			state := &models.State{Extra: map[string]any{"custom": value}}
			if _, err := marshalStateForWrite(state); err == nil {
				t.Fatal("malformed custom YAML accepted")
			} else if !strings.Contains(err.Error(), "parseable state YAML") {
				t.Fatalf("unexpected refusal: %v", err)
			}
		})
	}
}

func TestMarshalStateForWriteCustomNodeRoundTrip(t *testing.T) {
	t.Parallel()
	state := &models.State{Extra: map[string]any{
		"custom": &yaml.Node{Kind: yaml.ScalarNode, Style: yaml.DoubleQuotedStyle, Value: "\n  text\n"},
	}}
	data, err := marshalStateForWrite(state)
	if err != nil {
		t.Fatal(err)
	}
	var decoded models.State
	if err := yaml.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Extra["custom"] != "\n  text\n" {
		t.Fatalf("custom text changed: %#v", decoded.Extra["custom"])
	}
}

type writeTextValue string

func (v writeTextValue) MarshalText() ([]byte, error) { return []byte("text:" + v), nil }

type writeZeroValue struct{ Value string }

func (writeZeroValue) IsZero() bool { return true }

func TestMarshalStateForWritePreservesSchemaAndMethods(t *testing.T) {
	t.Parallel()
	type fixture struct {
		Text   writeTextValue    `yaml:"text"`
		Zero   writeZeroValue    `yaml:"zero,omitempty"`
		Nil    *string           `yaml:"nil"`
		Empty  []string          `yaml:"empty,omitempty"`
		Inline map[string]string `yaml:",inline"`
		Marker struct {
			Ignored string `yaml:"-"`
		} `yaml:"marker,omitempty"`
	}
	value := fixture{
		Text: "value", Zero: writeZeroValue{Value: "hidden"}, Inline: map[string]string{"inline": "kept"},
	}
	value.Marker.Ignored = "affects omission, never serialized"
	state := &models.State{Extra: map[string]any{"fixture": value}}
	want, err := yaml.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	got, err := marshalStateForWrite(state)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("schema or method behavior changed\ngot:\n%s\nwant:\n%s", got, want)
	}
	var decoded models.State
	if err := yaml.Unmarshal(got, &decoded); err != nil {
		t.Fatal(err)
	}
	expected := map[string]any{"text": "text:value", "nil": nil, "inline": "kept", "marker": map[string]any{}}
	if !reflect.DeepEqual(decoded.Extra["fixture"], expected) {
		t.Fatalf("schema values: %#v", decoded.Extra["fixture"])
	}
}

func TestMarshalStateForWriteRiskyKeysAndInputImmutability(t *testing.T) {
	t.Parallel()
	reason := "\n  reason\n"
	values := map[string]string{"\n  key\n": "\n\nvalue\n", "ordinary": "sibling"}
	state := &models.State{
		Tasks: []models.Task{{ID: "task-1", RejectionReason: &reason}},
		Extra: map[string]any{"keys": values},
	}
	before, err := yaml.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	data, err := marshalStateForWrite(state)
	if err != nil {
		t.Fatal(err)
	}
	after, err := yaml.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) || state.Tasks[0].RejectionReason != &reason || reason != "\n  reason\n" {
		t.Fatal("marshal modified source state or pointer identity")
	}
	var decoded models.State
	if err := yaml.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	expected := map[string]any{"\n  key\n": "\n\nvalue\n", "ordinary": "sibling"}
	if !reflect.DeepEqual(decoded.Extra["keys"], expected) || decoded.Tasks[0].RejectionReason == nil || *decoded.Tasks[0].RejectionReason != reason {
		t.Fatalf("keys or typed scalar changed: %#v, %#v", decoded.Extra["keys"], decoded.Tasks[0].RejectionReason)
	}
}

func TestMarshalStateForWriteTypedNilInterfaces(t *testing.T) {
	t.Parallel()
	var text *string
	state := &models.State{Extra: map[string]any{
		"nil": text, "nested": []any{map[string]any{"nil": text}},
	}}
	want, err := yaml.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	got, err := marshalStateForWrite(state)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("typed nil serialization changed\ngot:\n%s\nwant:\n%s", got, want)
	}
	var decoded models.State
	if err := yaml.Unmarshal(got, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Extra["nil"] != nil || !reflect.DeepEqual(decoded.Extra["nested"], []any{map[string]any{"nil": nil}}) {
		t.Fatalf("typed nil did not remain null: %#v", decoded.Extra)
	}
}

type writeInlineDecoded struct {
	Value string `yaml:"value"`
}

func (*writeInlineDecoded) UnmarshalYAML(*yaml.Node) error { return nil }

func TestMarshalStateForWritePreservesInlineUnmarshalSchema(t *testing.T) {
	type valueFixture struct {
		Inline  writeInlineDecoded `yaml:",inline"`
		Visible string             `yaml:"visible"`
	}
	type pointerFixture struct {
		Inline  *writeInlineDecoded `yaml:",inline"`
		Visible string              `yaml:"visible"`
	}
	for _, value := range []any{
		valueFixture{Inline: writeInlineDecoded{Value: "originally omitted"}, Visible: "kept"},
		pointerFixture{Inline: &writeInlineDecoded{Value: "originally omitted"}, Visible: "kept"},
		pointerFixture{Visible: "kept"},
	} {
		state := &models.State{Extra: map[string]any{"fixture": value}}
		want, err := yaml.Marshal(state)
		if err != nil {
			t.Fatal(err)
		}
		got, err := marshalStateForWrite(state)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("inline unmarshal schema changed\ngot:\n%s\nwant:\n%s", got, want)
		}
		var decoded models.State
		if err := yaml.Unmarshal(got, &decoded); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(decoded.Extra["fixture"], map[string]any{"visible": "kept"}) {
			t.Fatalf("previously omitted inline field persisted: %#v", decoded.Extra["fixture"])
		}
	}
}

func TestMarshalStateForWriteLifecycleRoundTrip(t *testing.T) {
	for _, repaired := range []bool{false, true} {
		state := largeMarshalState(1, repaired)
		before, err := yaml.Marshal(state)
		if err != nil {
			t.Fatal(err)
		}
		got, err := marshalStateForWrite(state)
		if err != nil {
			t.Fatal(err)
		}
		if !repaired && !bytes.Equal(before, got) {
			t.Fatalf("ordinary lifecycle schema changed\ngot:\n%s\nwant:\n%s", got, before)
		}
		var decoded models.State
		if err := yaml.Unmarshal(got, &decoded); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(decoded.Tasks[0], state.Tasks[0]) {
			t.Fatalf("lifecycle-bearing task changed: got %#v, want %#v", decoded.Tasks[0], state.Tasks[0])
		}
		after, err := yaml.Marshal(state)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("source state changed: %v", err)
		}
	}
}

// This resource regression compares allocations in the complete write marshal
// against the same emitter/hygiene work and a standalone independent decode.
// It avoids a wall-clock threshold and rejects paying for another YAML tree.
func TestMarshalStateForWriteAvoidsFullParseAllocations(t *testing.T) {
	state := largeMarshalState(128, false)
	var emitted []byte
	baseline := testing.AllocsPerRun(2, func() {
		if err := statehygiene.ValidateState(state); err != nil {
			t.Fatal(err)
		}
		var err error
		emitted, err = yaml.Marshal(state)
		if err != nil {
			t.Fatal(err)
		}
	})
	parse := testing.AllocsPerRun(2, func() {
		var decoded any
		if err := yaml.Unmarshal(emitted, &decoded); err != nil {
			t.Fatal(err)
		}
	})
	actual := testing.AllocsPerRun(2, func() {
		if _, err := marshalStateForWrite(state); err != nil {
			t.Fatal(err)
		}
	})
	t.Logf("emitter+hygiene=%.0f, standalone parse=%.0f, write marshal=%.0f allocations", baseline, parse, actual)
	if overhead := actual - baseline; overhead >= parse/2 {
		t.Fatalf("post-marshal overhead %.0f >= half a full parse (%.0f allocations)", overhead, parse/2)
	}
}

func largeMarshalState(count int, repaired bool) *models.State {
	state := &models.State{Agents: map[string]models.Agent{}}
	payload := strings.Repeat("bounded orchestration evidence with Unicode λ\n", 240)
	for i := range count {
		reason := "review complete\nall checks recorded\n"
		if repaired {
			reason = "\n---\nBlockers: 1\n- revise boundary"
		}
		state.Tasks = append(state.Tasks, models.Task{
			ID: fmt.Sprintf("task-%d", i), Description: "example task", RejectionReason: &reason,
			History: []models.TaskHistoryEntry{{Event: models.TaskEventRejected, Reason: &reason}},
			Extra:   map[string]any{"payload": payload},
			Lifecycle: &models.TaskLifecycle{
				Revision: 2, CompletionSequence: 1,
				Receipts: []models.LifecycleReceipt{{
					LifecycleIdentity: models.LifecycleIdentity{
						Operation: "submit-for-review", Actor: "coder-1", RequestID: fmt.Sprintf("request-%d", i),
						ExpectedTransition: "previous-boundary", PayloadDigest: "payload-digest",
					},
					Sequence: 1, TransitionID: "completed-boundary",
					Projection: models.LifecycleProjection{ReleasedDoer: true, ReviewCommit: "review-commit"},
				}},
				Preparation: &models.LifecyclePreparation{
					LifecycleIdentity: models.LifecycleIdentity{
						Operation: "wt-merge", Actor: "orchestrator-1", RequestID: fmt.Sprintf("merge-%d", i),
						ExpectedTransition: "completed-boundary", PayloadDigest: "merge-digest",
					},
					Boundary: "prepared-boundary",
				},
			},
		})
	}
	return state
}

func BenchmarkMarshalStateForWriteLarge(b *testing.B) {
	for _, repaired := range []bool{false, true} {
		b.Run(fmt.Sprintf("repaired=%t", repaired), func(b *testing.B) {
			state := largeMarshalState(300, repaired)
			data, err := marshalStateForWrite(state)
			if err != nil {
				b.Fatal(err)
			}
			b.SetBytes(int64(len(data)))
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := marshalStateForWrite(state); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
