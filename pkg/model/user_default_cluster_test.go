package model

import (
	"fmt"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func setupUserDefaultClusterDB(t *testing.T) {
	t.Helper()
	testDB, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:user-default-cluster-%d?mode=memory&cache=shared", time.Now().UnixNano())))
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	if err := testDB.AutoMigrate(&User{}); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	originalDB := DB
	DB = testDB
	t.Cleanup(func() {
		DB = originalDB
		if sqlDB, dbErr := testDB.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
	})
}

func TestSetUserDefaultCluster(t *testing.T) {
	setupUserDefaultClusterDB(t)

	user := User{Username: "carol", Provider: "password", Enabled: true}
	if err := DB.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() { InvalidateUserCache(uint64(user.ID)) })

	// Prime the LRU cache so the invalidation inside SetUserDefaultCluster
	// is exercised.
	if _, err := GetUserByIDCached(uint64(user.ID)); err != nil {
		t.Fatalf("prime user cache: %v", err)
	}

	if err := SetUserDefaultCluster(user.ID, "staging"); err != nil {
		t.Fatalf("set default cluster: %v", err)
	}
	got, err := GetUserByID(uint64(user.ID))
	if err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if got.DefaultCluster != "staging" {
		t.Fatalf("default cluster = %q, want staging", got.DefaultCluster)
	}
	cached, err := GetUserByIDCached(uint64(user.ID))
	if err != nil {
		t.Fatalf("cached user: %v", err)
	}
	if cached.DefaultCluster != "staging" {
		t.Fatalf("cached default cluster = %q, want staging (cache not invalidated)", cached.DefaultCluster)
	}

	if err := SetUserDefaultCluster(user.ID, ""); err != nil {
		t.Fatalf("clear default cluster: %v", err)
	}
	got, err = GetUserByID(uint64(user.ID))
	if err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if got.DefaultCluster != "" {
		t.Fatalf("default cluster after clear = %q, want empty", got.DefaultCluster)
	}
}

func TestFindWithSubOrUpsertUserPreservesDefaultCluster(t *testing.T) {
	setupUserDefaultClusterDB(t)

	first := User{Username: "dana", Provider: "github", Sub: "sub-1", Enabled: true}
	if err := DB.Create(&first).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() { InvalidateUserCache(uint64(first.ID)) })
	if err := SetUserDefaultCluster(first.ID, "prod"); err != nil {
		t.Fatalf("set default cluster: %v", err)
	}

	relogin := User{Username: "dana", Provider: "github", Sub: "sub-1"}
	if err := FindWithSubOrUpsertUser(&relogin); err != nil {
		t.Fatalf("upsert on re-login: %v", err)
	}
	got, err := GetUserByUsername("dana")
	if err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if got.DefaultCluster != "prod" {
		t.Fatalf("default cluster after re-login = %q, want prod", got.DefaultCluster)
	}
}

func TestUpsertLDAPUserPreservesDefaultCluster(t *testing.T) {
	setupUserDefaultClusterDB(t)

	existing := User{Username: "eve", Provider: AuthProviderLDAP, Enabled: true}
	if err := DB.Create(&existing).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() { InvalidateUserCache(uint64(existing.ID)) })
	if err := SetUserDefaultCluster(existing.ID, "dev"); err != nil {
		t.Fatalf("set default cluster: %v", err)
	}

	relogin := &User{Username: "eve"}
	if _, err := UpsertLDAPUser(relogin); err != nil {
		t.Fatalf("upsert on re-login: %v", err)
	}
	got, err := GetUserByUsername("eve")
	if err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if got.DefaultCluster != "dev" {
		t.Fatalf("default cluster after re-login = %q, want dev", got.DefaultCluster)
	}
}
