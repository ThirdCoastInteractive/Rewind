package db

import (
	"strings"
	"testing"
)

func TestParseTenantEmptyIsOSS(t *testing.T) {
	if ParseTenant("") != OSSTenant() {
		t.Fatal("empty tenant must be OSS")
	}
	if !OSSTenant().Valid {
		t.Fatal("OSS tenant must be a real UUID value")
	}
}

func TestParseTenantInvalid(t *testing.T) {
	if ParseTenant("nope").Valid {
		t.Fatal("invalid tenant must not be valid")
	}
}

func TestListVideosPaginatedSQLScopesTenant(t *testing.T) {
	if !strings.Contains(listVideosPaginated, "v.tenant_id =") {
		t.Fatal("paginated list must fail-closed on tenant_id")
	}
}
