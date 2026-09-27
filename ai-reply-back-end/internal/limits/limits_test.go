package limits

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aireply/ai-reply-back-end/internal/domain"
)

type fakeStore struct {
	values  map[string]string
	readErr error
	reads   int
}

func (f *fakeStore) Setting(_ context.Context, key string) (string, error) {
	f.reads++
	if f.readErr != nil {
		return "", f.readErr
	}
	v, ok := f.values[key]
	if !ok {
		return "", domain.ErrNotFound
	}
	return v, nil
}

func (f *fakeStore) SetSettings(_ context.Context, values map[string]string) error {
	for k, v := range values {
		f.values[k] = v
	}
	return nil
}

var env = Limits{SourceChars: 300, InstructionChars: 400, MaxOutputTokens: 180}

func TestDefaultsApplyWhenNothingIsStored(t *testing.T) {
	svc := New(&fakeStore{values: map[string]string{}}, env)
	if got := svc.Current(context.Background()); got != env {
		t.Fatalf("got %+v, want the deployment defaults %+v", got, env)
	}
}

func TestAdminValueWinsOverEnvironment(t *testing.T) {
	store := &fakeStore{values: map[string]string{KeySourceChars: "400"}}
	got := New(store, env).Current(context.Background())
	if got.SourceChars != 400 {
		t.Fatalf("source = %d, want the admin value 400", got.SourceChars)
	}
	if got.InstructionChars != 400 || got.MaxOutputTokens != 180 {
		t.Fatalf("untouched values must keep their defaults: %+v", got)
	}
}

func TestBrokenStoredValueFallsBackToDefault(t *testing.T) {
	for _, raw := range []string{"", "abc", "0", "-5", "999999"} {
		store := &fakeStore{values: map[string]string{KeySourceChars: raw}}
		if got := New(store, env).Current(context.Background()).SourceChars; got != env.SourceChars {
			t.Fatalf("stored %q gave %d, want the default %d", raw, got, env.SourceChars)
		}
	}
}

func TestInvalidEnvironmentIsNotTrusted(t *testing.T) {
	svc := New(&fakeStore{values: map[string]string{}}, Limits{})
	want := Limits{SourceChars: DefaultSourceChars, InstructionChars: DefaultInstructionChars,
		MaxOutputTokens: DefaultMaxOutputTokens}
	if got := svc.Current(context.Background()); got != want {
		t.Fatalf("got %+v, want code defaults %+v", got, want)
	}
}

func TestUpdateValidatesEveryField(t *testing.T) {
	svc := New(&fakeStore{values: map[string]string{}}, env)
	cases := []Limits{
		{SourceChars: 10, InstructionChars: 400, MaxOutputTokens: 180},
		{SourceChars: 400, InstructionChars: 5000, MaxOutputTokens: 180},
		{SourceChars: 400, InstructionChars: 400, MaxOutputTokens: 1},
	}
	for _, c := range cases {
		_, err := svc.Update(context.Background(), c)
		var fieldErr *FieldError
		if !errors.As(err, &fieldErr) {
			t.Fatalf("%+v: expected a FieldError, got %v", c, err)
		}
		if !errors.Is(err, domain.ErrInvalidRequest) {
			t.Fatalf("%+v: a FieldError must read as an invalid request", c)
		}
	}
	if got := svc.Current(context.Background()); got != env {
		t.Fatalf("a rejected update changed the limits: %+v", got)
	}
}

func TestUpdateIsVisibleImmediately(t *testing.T) {
	store := &fakeStore{values: map[string]string{}}
	svc := New(store, env)
	_ = svc.Current(context.Background()) // warm the cache
	next := Limits{SourceChars: 450, InstructionChars: 350, MaxOutputTokens: 200}
	if _, err := svc.Update(context.Background(), next); err != nil {
		t.Fatal(err)
	}
	if got := svc.Current(context.Background()); got != next {
		t.Fatalf("got %+v right after update, want %+v", got, next)
	}
	if store.values[KeySourceChars] != "450" {
		t.Fatalf("not persisted: %v", store.values)
	}
}

func TestReadsAreCachedAndFailuresKeepLastKnownValues(t *testing.T) {
	store := &fakeStore{values: map[string]string{KeySourceChars: "420"}}
	now := time.Unix(1000, 0)
	svc := New(store, env)
	svc.now = func() time.Time { return now }

	first := svc.Current(context.Background())
	reads := store.reads
	_ = svc.Current(context.Background())
	if store.reads != reads {
		t.Fatal("a second read inside the TTL went to the store")
	}

	now = now.Add(time.Minute)
	store.readErr = errors.New("database is locked")
	if got := svc.Current(context.Background()); got != first {
		t.Fatalf("a failed refresh changed the limits: %+v", got)
	}
}

func TestOverriddenReportsWhereEachValueComesFrom(t *testing.T) {
	store := &fakeStore{values: map[string]string{KeySourceChars: "400"}}
	got, err := New(store, env).Overridden(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !got[KeySourceChars] || got[KeyInstructionChars] || got[KeyMaxOutputTokens] {
		t.Fatalf("overridden = %v", got)
	}
}
