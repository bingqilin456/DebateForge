package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
)

func TestErrorsExposeFailedVersionWithoutSQLOrSecret(t *testing.T) {
	cause := errors.New("sensitive-dsn SELECT private-data")
	wrapped := safeError("执行迁移失败", &goose.PartialError{
		Failed: &goose.MigrationResult{Source: &goose.Source{Version: 4}}, Err: cause,
	})
	if !errors.Is(wrapped, cause) || !strings.Contains(wrapped.Error(), "version=4") {
		t.Fatal("未保留失败版本和内部原因")
	}
	for _, rendered := range []string{wrapped.Error(), fmt.Sprintf("%+v", wrapped)} {
		if strings.Contains(rendered, "sensitive") || strings.Contains(rendered, "SELECT") {
			t.Fatal("错误泄露 SQL 或凭据")
		}
	}
}
