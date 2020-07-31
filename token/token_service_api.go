package token

type PayLoad interface{}
type CustomToken interface{}

// Token.Manager interface is an API for creating and verifying tokens
type Manager interface {
	CreateToken(key PayLoad) (token CustomToken, err error)
	VerifyToken(token CustomToken, key PayLoad) (err error)
}
