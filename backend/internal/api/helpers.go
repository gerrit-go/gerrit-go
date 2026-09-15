package api

import (
	"context"

	"gerrit-go/internal/store"

	"golang.org/x/crypto/bcrypt"
)

type ctxKey int

const accountKey ctxKey = 1

func withAccount(ctx context.Context, a *store.Account) context.Context {
	return context.WithValue(ctx, accountKey, a)
}

func accountFrom(ctx context.Context) *store.Account {
	a, _ := ctx.Value(accountKey).(*store.Account)
	return a
}

func hashPassword(pw string) string {
	hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	if err != nil {
		panic(err)
	}
	return string(hash)
}
