package auth

import "testing"

func TestRoles(t *testing.T) {
	if !RoleAdmin.AtLeast(RoleLibrarian) || !RoleLibrarian.AtLeast(RoleMember) || !RoleMember.AtLeast(RoleMember) {
		t.Fatal("ordering")
	}
	if RoleMember.AtLeast(RoleLibrarian) || RoleLibrarian.AtLeast(RoleAdmin) {
		t.Fatal("ordering (reverse)")
	}
	if Role("owner").Valid() || !RoleMember.Valid() {
		t.Fatal("valid")
	}
	if Role("").AtLeast(RoleMember) {
		t.Fatal("empty role must not pass")
	}
}
