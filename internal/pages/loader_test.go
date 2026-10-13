package pages

import (
	"context"
	"encoding/json"
	"errors"
	"go-quick/internal/workspace"
	"testing"
)

const testManifest = `[
 {"id":"layout:root","kind":"layout","pattern":"/","data":{"shell":{"source":"shell"}}},
 {"id":"layout:project","kind":"layout","parent":"layout:root","pattern":"/projects/[projectId]","data":{"project":{"source":"projects.detail","params":{"projectId":{"routeParam":"projectId"}}}}},
 {"id":"page:order","kind":"page","parent":"layout:project","pattern":"/projects/[projectId]/orders/[orderId]","data":{"order":{"source":"orders.detail","params":{"projectId":{"routeParam":"projectId"},"orderId":{"routeParam":"orderId"}}}}}
]`

func TestResolveScopesBeforeRender(t *testing.T) {
	loader, err := New([]byte(testManifest), "test-build", workspace.Providers(workspace.NewStore()))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := loader.Load(context.Background(), "/projects/studio/orders/SH-1042")
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Scopes) != 3 || snapshot.PageID != "page:order" {
		t.Fatalf("incorrect scopes: %+v", snapshot)
	}
	if snapshot.Scopes["page:order"]["order"].(workspace.Order).Customer != "林夏" {
		t.Fatal("wrong page data")
	}
	if _, err := json.Marshal(snapshot); err != nil {
		t.Fatal(err)
	}
}

func TestCrossProjectOrderIsNotFound(t *testing.T) {
	loader, _ := New([]byte(testManifest), "test", workspace.Providers(workspace.NewStore()))
	_, err := loader.Load(context.Background(), "/projects/weekend/orders/SH-1042")
	if !errors.Is(err, workspace.ErrNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestExternalAndMalformedURLsAreRejected(t *testing.T) {
	loader, _ := New([]byte(testManifest), "test", workspace.Providers(workspace.NewStore()))
	for _, url := range []string{"https://example.com/projects/studio/orders/SH-1042", "//example.com/x", "/projects/studio%2Fweekend/orders/SH-1042", "/projects/studio/orders/SH-1042#fragment"} {
		if _, err := loader.Load(context.Background(), url); err == nil {
			t.Errorf("accepted %q", url)
		}
	}
}

func TestInvalidProviderContractFailsAtStartup(t *testing.T) {
	for _, manifest := range []string{
		`[{"id":"page:root","kind":"page","pattern":"/","data":{"secret":{"source":"missing"}}}]`,
		`[{"id":"page:root","kind":"page","pattern":"/","data":{"order":{"source":"orders.detail"}}}]`,
		`[{"id":"page:root","kind":"page","parent":"missing","pattern":"/","data":{}}]`,
	} {
		if _, err := New([]byte(manifest), "test", workspace.Providers(workspace.NewStore())); err == nil {
			t.Errorf("accepted invalid manifest %s", manifest)
		}
	}
}

func TestCanceledLoadDoesNotRunProviders(t *testing.T) {
	providers := workspace.Providers(workspace.NewStore())
	p := providers["shell"]
	p.Load = func(context.Context, map[string]string) (any, error) {
		t.Fatal("provider ran after cancellation")
		return nil, nil
	}
	providers["shell"] = p
	loader, _ := New([]byte(testManifest), "test", providers)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := loader.Load(ctx, "/projects/studio/orders/SH-1042"); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}
