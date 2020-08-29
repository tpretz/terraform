package http

import (
	"testing"
	"time"

	"github.com/hashicorp/terraform/configs"
	"github.com/zclconf/go-cty/cty"

	"github.com/hashicorp/terraform/backend"
)

func TestBackend_impl(t *testing.T) {
	var _ backend.Backend = new(Backend)
}

func TestHTTPClientFactory(t *testing.T) {
	// defaults

	conf := map[string]cty.Value{
		"address": cty.StringVal("http://127.0.0.1:8888/foo"),
	}
	b := backend.TestBackendConfig(t, New(), configs.SynthBody("synth", conf)).(*Backend)
	client := b.client

	if client == nil {
		t.Fatal("Unexpected failure, address")
	}
	if client.URL.String() != "http://127.0.0.1:8888/foo" {
		t.Fatalf("Expected address \"%s\", got \"%s\"", conf["address"], client.URL.String())
	}
	if client.UpdateMethod != "POST" {
		t.Fatalf("Expected update_method \"%s\", got \"%s\"", "POST", client.UpdateMethod)
	}
	if client.LockURL != nil || client.LockMethod != "LOCK" {
		t.Fatal("Unexpected lock_address or lock_method")
	}
	if client.UnlockURL != nil || client.UnlockMethod != "UNLOCK" {
		t.Fatal("Unexpected unlock_address or unlock_method")
	}
	if client.Username != "" || client.Password != "" {
		t.Fatal("Unexpected username or password")
	}

	// custom
	conf = map[string]cty.Value{
		"address":                  cty.StringVal("http://127.0.0.1:8888/foo"),
		"update_method":            cty.StringVal("BLAH"),
		"lock_address":             cty.StringVal("http://127.0.0.1:8888/bar"),
		"lock_method":              cty.StringVal("BLIP"),
		"unlock_address":           cty.StringVal("http://127.0.0.1:8888/baz"),
		"unlock_method":            cty.StringVal("BLOOP"),
		"username":                 cty.StringVal("user"),
		"password":                 cty.StringVal("pass"),
		"retry_max":                cty.StringVal("999"),
		"retry_wait_min":           cty.StringVal("15"),
		"retry_wait_max":           cty.StringVal("150"),
		"workspace_enabled":        cty.BoolVal(true),
		"workspace_path_element":   cty.StringVal("cheese"),
		"workspace_list_address":   cty.StringVal("http://127.0.0.1:8888/workspace/list"),
		"workspace_delete_address": cty.StringVal("http://127.0.0.1:8888/workspace/cheese/delete"),
		"headers": cty.ObjectVal(map[string]cty.Value{
			"X-TOKEN": cty.StringVal("secret"),
		}),
	}

	b = backend.TestBackendConfig(t, New(), configs.SynthBody("synth", conf)).(*Backend)
	client = b.client

	if client == nil {
		t.Fatal("Unexpected failure, update_method")
	}
	if client.UpdateMethod != "BLAH" {
		t.Fatalf("Expected update_method \"%s\", got \"%s\"", "BLAH", client.UpdateMethod)
	}
	if client.LockURL.String() != conf["lock_address"].AsString() || client.LockMethod != "BLIP" {
		t.Fatalf("Unexpected lock_address \"%s\" vs \"%s\" or lock_method \"%s\" vs \"%s\"", client.LockURL.String(),
			conf["lock_address"].AsString(), client.LockMethod, conf["lock_method"])
	}
	if client.UnlockURL.String() != conf["unlock_address"].AsString() || client.UnlockMethod != "BLOOP" {
		t.Fatalf("Unexpected unlock_address \"%s\" vs \"%s\" or unlock_method \"%s\" vs \"%s\"", client.UnlockURL.String(),
			conf["unlock_address"].AsString(), client.UnlockMethod, conf["unlock_method"])
	}
	if client.Username != "user" || client.Password != "pass" {
		t.Fatalf("Unexpected username \"%s\" vs \"%s\" or password \"%s\" vs \"%s\"", client.Username, conf["username"],
			client.Password, conf["password"])
	}
	if client.Client.RetryMax != 999 {
		t.Fatalf("Expected retry_max \"%d\", got \"%d\"", 999, client.Client.RetryMax)
	}
	if client.Client.RetryWaitMin != 15*time.Second {
		t.Fatalf("Expected retry_wait_min \"%s\", got \"%s\"", 15*time.Second, client.Client.RetryWaitMin)
	}
	if client.Client.RetryWaitMax != 150*time.Second {
		t.Fatalf("Expected retry_wait_max \"%s\", got \"%s\"", 150*time.Second, client.Client.RetryWaitMax)
	}
	if b.workspaceEnabled != true {
		t.Fatalf("Expected workspace_enabled to be \"%s\" got \"%t\"", conf["workspace_enabled"].AsString(),
			b.workspaceEnabled)
	}
	if b.workspacePathElement != "cheese" {
		t.Fatalf("Expected workspace_path_element \"%s\", got \"%s\"", conf["workspace_path_element"].AsString(),
			b.workspacePathElement)
	}
	if client.WorkspaceListURL.String() != conf["workspace_list_address"].AsString() {
		t.Fatalf("Unexpected workspace_list_url \"%s\", got \"%s\"", conf["workspace_list_address"].AsString(),
			client.WorkspaceListURL.String())
	}
	if client.WorkspaceDeleteURL.String() != conf["workspace_delete_address"].AsString() {
		t.Fatalf("Unexpected workspace_delete_url \"%s\", got \"%s\"", conf["workspace_delete_address"].AsString(),
			client.WorkspaceDeleteURL.String())
	}
	if client.Headers == nil {
		t.Fatalf("Unexpected nil map for client headers")
	}
	if client.Headers["X-TOKEN"] != "secret" {
		t.Fatalf("Unexpected headers entry \"%s\", got \"%s\"", "secret",
			client.Headers["X-TOKEN"])
	}
}
