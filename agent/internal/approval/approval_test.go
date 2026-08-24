package approval

import (
	"testing"
	"time"

	"qnap-ai-control-suite/agent/internal/operations"
)

func TestTicketConsumesBoundRetryAndIsSingleUse(t *testing.T) {
	now := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)
	manager := NewWithOptions(Options{TTL: 10 * time.Minute, Now: func() time.Time { return now }, ID: func() string { return "ticket" }})
	binding := BindingFor("POST", "/v1/system/reboot", []byte(`{"action":"reboot"}`), "system.power")
	ticket := manager.Create("request", operations.Operation{Name: "system.power", Risk: operations.Sensitive, Target: "reboot", Summary: "sensitive reboot"}, binding)
	if ticket.State != Pending || ticket.ID != "ticket" {
		t.Fatalf("ticket=%+v", ticket)
	}
	if used, err := manager.Consume(ticket.ID, binding); err != nil || used.State != Used {
		t.Fatalf("consume=%+v err=%v", used, err)
	}
	if _, err := manager.Consume(ticket.ID, binding); err != ErrUsed {
		t.Fatalf("second consume error=%v", err)
	}
}

func TestTicketBindsExactRequestAndExpires(t *testing.T) {
	now := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)
	manager := NewWithOptions(Options{TTL: time.Minute, Now: func() time.Time { return now }, ID: func() string { return "ticket" }})
	binding := BindingFor("POST", "/v1/exec", []byte(`{"argv":["/bin/true"]}`), "command.exec")
	ticket := manager.Create("request", operations.Operation{Name: "command.exec", Risk: operations.Sensitive, Summary: "sensitive command"}, binding)
	other := BindingFor("POST", "/v1/exec", []byte(`{"argv":["/bin/false"]}`), "command.exec")
	if _, err := manager.Consume(ticket.ID, other); err != ErrBindingMismatch {
		t.Fatalf("binding mismatch error=%v", err)
	}
	now = now.Add(time.Minute)
	if _, err := manager.Consume(ticket.ID, binding); err != ErrExpired {
		t.Fatalf("expiry error=%v", err)
	}
}

func TestOptionalDecisionCanDenyATicket(t *testing.T) {
	manager := NewWithOptions(Options{ID: func() string { return "ticket" }})
	binding := BindingFor("POST", "/v1/exec", []byte(`{"argv":["/bin/true"]}`), "command.exec")
	ticket := manager.Create("request", operations.Operation{Name: "command.exec", Risk: operations.Sensitive}, binding)
	if denied, err := manager.Decide(ticket.ID, false); err != nil || denied.State != Denied {
		t.Fatalf("decision=%+v err=%v", denied, err)
	}
	if _, err := manager.Consume(ticket.ID, binding); err != ErrDenied {
		t.Fatalf("consume denied ticket error=%v", err)
	}
}
