package auth

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	driver "github.com/go-sql-driver/mysql"
)

func TestIsDuplicateEntryDetectsOnlyMySQL1062(t *testing.T) {
	duplicate := &driver.MySQLError{Number: 1062, Message: "Duplicate entry 'alice' for key 'users.uq_users_username'"}
	if !isDuplicateEntry(duplicate) {
		t.Fatal("MySQL 1062 must map to a username conflict")
	}
	if !isDuplicateEntry(fmt.Errorf("创建用户失败: %w", duplicate)) {
		t.Fatal("wrapped 1062 must still be detected")
	}
	for name, err := range map[string]error{
		"nil":          nil,
		"other code":   &driver.MySQLError{Number: 1045, Message: "Access denied"},
		"plain error":  errors.New("Duplicate entry for key"),
		"storage down": storageFailure{},
	} {
		t.Run(name, func(t *testing.T) {
			if isDuplicateEntry(err) {
				t.Fatal("non-1062 error must not be treated as a conflict")
			}
		})
	}
}

func TestRepositoryErrorStringsAreControlled(t *testing.T) {
	// 驱动错误保留 cause 供 errors.Is/As，但字符串不能包含 SQL、DSN 或凭据。
	origin := &driver.MySQLError{Number: 1146, Message: "Table 'siyige_test.users' doesn't exist, dsn=siyige_test_app:secret@tcp(127.0.0.1:13307)"}
	err := storeError("创建用户失败", origin)
	if err.Error() != "创建用户失败" {
		t.Fatalf("error string leaked internals: %q", err.Error())
	}
	if !errors.Is(err, origin) {
		t.Fatal("cause must be preserved for diagnostics")
	}
	for _, forbidden := range []string{"siyige_test_app", "secret", "127.0.0.1", "doesn't exist", "INSERT INTO"} {
		if strings.Contains(err.Error(), forbidden) {
			t.Fatalf("error string leaked %q: %s", forbidden, err.Error())
		}
	}
	if errors.Is(err, ErrNotFound) {
		t.Fatal("storage failure must not look like a missing row")
	}
}
