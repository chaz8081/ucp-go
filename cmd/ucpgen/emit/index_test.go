package emit

import "testing"

func TestPackageForSchema(t *testing.T) {
	cases := map[string]struct{ pkg, imp string }{
		"ucp.json":                        {"ucp", "github.com/chaz8081/ucp-go"},
		"capability.json":                 {"ucp", "github.com/chaz8081/ucp-go"},
		"shopping/cart.json":              {"shopping", "github.com/chaz8081/ucp-go/shopping"},
		"shopping/types/line_item.json":   {"types", "github.com/chaz8081/ucp-go/shopping/types"},
		"common/identity_linking.json":    {"common", "github.com/chaz8081/ucp-go/common"},
		"transports/embedded_config.json": {"transports", "github.com/chaz8081/ucp-go/transports"},
	}
	for rel, want := range cases {
		gotPkg, gotImp := PackageForSchema(rel, "github.com/chaz8081/ucp-go")
		if gotPkg != want.pkg || gotImp != want.imp {
			t.Errorf("PackageForSchema(%q) = (%q, %q), want (%q, %q)", rel, gotPkg, gotImp, want.pkg, want.imp)
		}
	}
}

func TestBuildTypeIndex(t *testing.T) {
	files := map[string]map[string]any{
		"shopping/types/line_item.json": {
			"title": "Line Item", "type": "object",
			"properties": map[string]any{"sku": map[string]any{"type": "string"}},
		},
		"capability.json": {
			"title": "Capability",
			"$defs": map[string]any{
				"base":            map[string]any{"type": "object"},
				"platform_schema": map[string]any{"type": "object"},
			},
		},
		"service.json": {
			"title": "Service",
			"$defs": map[string]any{"base": map[string]any{"type": "object"}},
		},
	}
	idx, err := BuildTypeIndex(files, "github.com/chaz8081/ucp-go")
	if err != nil {
		t.Fatalf("BuildTypeIndex: %v", err)
	}
	got, ok := idx.Lookup("shopping/types/line_item.json", "")
	if !ok || got.Name != "LineItem" || got.Package != "types" {
		t.Errorf("line_item file type = %+v, ok=%v", got, ok)
	}
	cb, _ := idx.Lookup("capability.json", "base")
	sb, _ := idx.Lookup("service.json", "base")
	if cb.Name != "CapabilityBase" || sb.Name != "ServiceBase" {
		t.Errorf("qualified names = %q / %q, want CapabilityBase / ServiceBase", cb.Name, sb.Name)
	}
	if cb.Package != "ucp" || cb.ImportPath != "github.com/chaz8081/ucp-go" {
		t.Errorf("capability base package = %+v", cb)
	}
	ps, _ := idx.Lookup("capability.json", "platform_schema")
	if ps.Name != "CapabilityPlatformSchema" {
		t.Errorf("platform_schema name = %q", ps.Name)
	}
	if _, ok := idx.Lookup("capability.json", ""); ok {
		t.Error("defs-only schema must not register a file-level type")
	}
}

func TestBuildTypeIndexRejectsCollision(t *testing.T) {
	files := map[string]map[string]any{
		"shopping/cart.json":   {"title": "Cart", "type": "object", "properties": map[string]any{}},
		"shopping/basket.json": {"title": "Cart", "type": "object", "properties": map[string]any{}},
	}
	if _, err := BuildTypeIndex(files, "m"); err == nil {
		t.Error("two file types with the same name in one package must error")
	}
}
