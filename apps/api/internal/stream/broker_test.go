package stream

import (
	"testing"
	"time"
)

func TestBroker_PublishDeliversToSubscriber(t *testing.T) {
	b := NewBroker()
	ch, unsubscribe := b.Subscribe()
	defer unsubscribe()

	b.publish("job.new", `{"job_group_id":"abc"}`)

	select {
	case ev := <-ch:
		if ev.Name != "job.new" || ev.Data != `{"job_group_id":"abc"}` {
			t.Errorf("got %+v, want job.new event with that payload", ev)
		}
		if ev.ID != 1 {
			t.Errorf("ID = %d, want 1 (first event)", ev.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for published event")
	}
}

func TestBroker_MultipleSubscribersAllReceive(t *testing.T) {
	b := NewBroker()
	ch1, unsub1 := b.Subscribe()
	defer unsub1()
	ch2, unsub2 := b.Subscribe()
	defer unsub2()

	b.publish("heartbeat", `{"ts":"now"}`)

	for i, ch := range []chan Event{ch1, ch2} {
		select {
		case <-ch:
		case <-time.After(time.Second):
			t.Fatalf("subscriber %d never received the event", i)
		}
	}
}

func TestBroker_UnsubscribeStopsDelivery(t *testing.T) {
	b := NewBroker()
	ch, unsubscribe := b.Subscribe()
	unsubscribe()

	b.publish("job.new", `{}`)

	select {
	case _, ok := <-ch:
		if ok {
			t.Error("expected the channel to be closed after unsubscribe, got a delivered event")
		}
	case <-time.After(time.Second):
		t.Fatal("channel was neither closed nor delivered to after unsubscribe")
	}
}

func TestBroker_UnsubscribeIsSafeToCallTwice(t *testing.T) {
	b := NewBroker()
	_, unsubscribe := b.Subscribe()
	unsubscribe()
	unsubscribe() // must not panic (double close)
}

func TestBroker_EventsSinceReturnsOnlyLaterEvents(t *testing.T) {
	b := NewBroker()
	b.publish("job.new", `{"n":1}`)
	b.publish("job.new", `{"n":2}`)
	b.publish("job.new", `{"n":3}`)

	got := b.EventsSince(1)
	if len(got) != 2 {
		t.Fatalf("EventsSince(1) returned %d events, want 2", len(got))
	}
	if got[0].Data != `{"n":2}` || got[1].Data != `{"n":3}` {
		t.Errorf("got %+v, want events 2 and 3 in order", got)
	}
}

func TestBroker_EventsSinceWithCurrentIDReturnsNothing(t *testing.T) {
	b := NewBroker()
	b.publish("job.new", `{}`)

	if got := b.EventsSince(1); len(got) != 0 {
		t.Errorf("EventsSince(1) after 1 event = %d results, want 0", len(got))
	}
}

func TestBroker_BufferIsCappedAtBufferSize(t *testing.T) {
	b := NewBroker()
	for i := 0; i < bufferSize+50; i++ {
		b.publish("job.new", `{}`)
	}

	got := b.EventsSince(0)
	if len(got) != bufferSize {
		t.Errorf("buffered event count = %d, want %d (capped)", len(got), bufferSize)
	}
	// The oldest surviving event's ID should reflect exactly bufferSize+50
	// total published, with only the newest bufferSize retained.
	if got[0].ID != int64(50+1) {
		t.Errorf("oldest retained event ID = %d, want %d", got[0].ID, 51)
	}
}

func TestBroker_SlowSubscriberDoesNotBlockPublish(t *testing.T) {
	b := NewBroker()
	ch, unsubscribe := b.Subscribe() // buffered 16, never drained here
	defer unsubscribe()
	_ = ch

	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			b.publish("job.new", `{}`)
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("publish blocked on a full, undrained subscriber channel")
	}
}
