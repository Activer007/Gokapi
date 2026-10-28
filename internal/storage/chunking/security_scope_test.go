//go:build test

package chunking

import (
	"strings"
	"testing"
)

func TestSEC032StorageScope(t *testing.T) {
	id := "shared-client-uuid"
	own := StorageID(id, 9132, "")
	if own == StorageID(id, 9133, "") {
		t.Fatal("users share storage ID")
	}
	if own == StorageID(own, 9133, "") {
		t.Fatal("external ID bypasses user scope")
	}
	if StorageID(id, 9132, "request") != id {
		t.Fatal("reservation ID compatibility changed")
	}
	if own == id || strings.ContainsAny(own, "/\\") {
		t.Fatal("unsafe scoped storage ID")
	}
	if getChunkFilePath(own) == getChunkFilePath(sanitiseUuid(own)) {
		t.Fatal("scoped storage may adopt an ownerless legacy chunk")
	}
	if StorageID("client/uuid", 9132, "") != StorageID("client_uuid", 9132, "") {
		t.Fatal("existing UUID normalization changed")
	}
}
