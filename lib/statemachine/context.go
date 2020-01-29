package statemachine

import "context"

type Context struct {
	ctx  context.Context
	send func(evt interface{}) error
}

func NewContext(ctx context.Context, send func(evt interface{})error) Context {
	return Context{ctx, send}
}

func (ctx *Context) Context() context.Context {
	return ctx.ctx
}

func (ctx *Context) Send(evt interface{}) error {
	return ctx.send(evt)
}
