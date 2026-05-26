package reload_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/doctrine/reload"
)

func TestClose_SignalsReloadSubscribersViaChannelClose(t *testing.T) {
	w, err := reload.New(reload.WatcherOpts{
		EventlogClient: &fakeEventlog{},
		ActiveAccessor: &recordingActive{},
		Parser:         &fakeParser{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	sub := w.SubscribeReloadEvents()
	w.Close()
	select {
	case _, ok := <-sub:
		if ok {
			t.Error("expected subscribed channel to be closed; got value with ok=true")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("subscriber channel not closed within 100ms after Watcher.Close")
	}
}

func TestClose_SignalsReloadFailedSubscribersViaChannelClose(t *testing.T) {
	w, err := reload.New(reload.WatcherOpts{
		EventlogClient: &fakeEventlog{},
		ActiveAccessor: &recordingActive{},
		Parser:         &fakeParser{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	sub := w.SubscribeReloadFailedEvents()
	w.Close()
	select {
	case _, ok := <-sub:
		if ok {
			t.Error("expected subscribed failed-channel to be closed; got value with ok=true")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("failed-event subscriber channel not closed within 100ms after Watcher.Close")
	}
}

func TestClose_MultipleSubscribers_AllReceiveCloseSignal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "max-scope.toml")
	if err := os.WriteFile(path, []byte(`x`), 0o644); err != nil {
		t.Fatal(err)
	}
	w, err := reload.New(reload.WatcherOpts{
		EventlogClient: &fakeEventlog{},
		ActiveAccessor: &recordingActive{},
		Parser:         &fakeParser{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := w.AddPath(path, ""); err != nil {
		t.Fatal(err)
	}
	subs := []<-chan reload.DoctrineReloaded{
		w.SubscribeReloadEvents(),
		w.SubscribeReloadEvents(),
		w.SubscribeReloadEvents(),
	}
	failedSubs := []<-chan reload.DoctrineReloadFailed{
		w.SubscribeReloadFailedEvents(),
		w.SubscribeReloadFailedEvents(),
	}
	w.Close()
	for i, s := range subs {
		select {
		case _, ok := <-s:
			if ok {
				t.Errorf("subs[%d]: expected closed channel; got value with ok=true", i)
			}
		case <-time.After(100 * time.Millisecond):
			t.Errorf("subs[%d]: not closed within 100ms", i)
		}
	}
	for i, s := range failedSubs {
		select {
		case _, ok := <-s:
			if ok {
				t.Errorf("failedSubs[%d]: expected closed channel; got value with ok=true", i)
			}
		case <-time.After(100 * time.Millisecond):
			t.Errorf("failedSubs[%d]: not closed within 100ms", i)
		}
	}
}

func TestClose_RaceAgainstBroadcast(t *testing.T) {
	if testing.Short() {
		t.Skip("close-race stress skipped in -short mode")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "max-scope.toml")
	if err := os.WriteFile(path, []byte(`x`), 0o644); err != nil {
		t.Fatal(err)
	}
	w, err := reload.New(reload.WatcherOpts{
		EventlogClient:   &fakeEventlog{},
		ActiveAccessor:   &recordingActive{},
		Parser:           &fakeParser{},
		Validator:        &fakeValidator{},
		BaselineProvider: fakeBaselineProvider{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := w.AddPath(path, ""); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		_ = w.SubscribeReloadEvents()
	}

	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
				_ = w.NotifyForce(path)
			}
		}
	}()

	time.Sleep(10 * time.Millisecond)

	w.Close()
	close(stop)
	<-done
}

func TestClose_Idempotent(t *testing.T) {
	w, err := reload.New(reload.WatcherOpts{
		EventlogClient: &fakeEventlog{},
		ActiveAccessor: &recordingActive{},
		Parser:         &fakeParser{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	sub := w.SubscribeReloadEvents()
	w.Close()

	select {
	case <-sub:
	case <-time.After(100 * time.Millisecond):
	}
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("second Close panicked: %v", r)
		}
	}()
	w.Close()
}
