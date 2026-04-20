package cliproxy

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"unsafe"

	internalusage "github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
	coreusage "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/usage"
)

var serviceUsagePersistenceTestMu sync.Mutex

type accountUsageStoreState struct {
	storageDir string
	accounts   reflect.Value
}

func TestServiceShutdownFlushesAccountUsage(t *testing.T) {
	serviceUsagePersistenceTestMu.Lock()
	defer serviceUsagePersistenceTestMu.Unlock()

	home := t.TempDir()
	t.Setenv("HOME", home)

	store := internalusage.GetAccountUsageStore()
	state := captureAccountUsageStoreState(store)
	t.Cleanup(func() {
		restoreAccountUsageStoreState(store, state)
	})
	resetAccountUsageStore(store)

	store.SetStorageDir("~/.cli-proxy-api")
	store.Record(coreusage.Record{
		Source: "shutdown@example.com",
		Model:  "gpt-5.4",
		Detail: coreusage.Detail{TotalTokens: 9},
	})

	service := &Service{}
	if err := service.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	path := filepath.Join(home, ".cli-proxy-api.usage", "account_usage.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read flushed usage file: %v", err)
	}
	if !strings.Contains(string(data), "shutdown@example.com") {
		t.Fatalf("usage file missing shutdown account entry: %s", data)
	}
}

func captureAccountUsageStoreState(store *internalusage.AccountUsageStore) accountUsageStoreState {
	v := reflect.ValueOf(store).Elem()
	return accountUsageStoreState{
		storageDir: fieldString(v.FieldByName("storageDir")),
		accounts:   cloneAccountsMap(v.FieldByName("accounts")),
	}
}

func restoreAccountUsageStoreState(store *internalusage.AccountUsageStore, state accountUsageStoreState) {
	v := reflect.ValueOf(store).Elem()
	setUnexportedField(v.FieldByName("storageDir"), reflect.ValueOf(state.storageDir))
	setUnexportedField(v.FieldByName("accounts"), cloneAccountsMap(state.accounts))
}

func resetAccountUsageStore(store *internalusage.AccountUsageStore) {
	v := reflect.ValueOf(store).Elem()
	accountsField := v.FieldByName("accounts")
	setUnexportedField(accountsField, reflect.MakeMap(accountsField.Type()))
	setUnexportedField(v.FieldByName("storageDir"), reflect.Zero(v.FieldByName("storageDir").Type()))
}

func cloneAccountsMap(accounts reflect.Value) reflect.Value {
	if !accounts.IsValid() {
		return reflect.Value{}
	}
	cloned := reflect.MakeMapWithSize(accounts.Type(), accounts.Len())
	iter := accounts.MapRange()
	for iter.Next() {
		key := iter.Key()
		value := iter.Value()
		copied := reflect.New(value.Elem().Type())
		copied.Elem().Set(value.Elem())

		models := value.Elem().FieldByName("Models")
		if models.IsValid() && !models.IsNil() {
			copiedModels := reflect.MakeMapWithSize(models.Type(), models.Len())
			modelIter := models.MapRange()
			for modelIter.Next() {
				copiedModels.SetMapIndex(modelIter.Key(), modelIter.Value())
			}
			copied.Elem().FieldByName("Models").Set(copiedModels)
		}

		cloned.SetMapIndex(key, copied)
	}
	return cloned
}

func fieldString(v reflect.Value) string {
	return reflect.NewAt(v.Type(), unsafe.Pointer(v.UnsafeAddr())).Elem().String()
}

func setUnexportedField(field reflect.Value, value reflect.Value) {
	reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Set(value)
}
