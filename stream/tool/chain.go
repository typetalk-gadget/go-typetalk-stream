package tool

import "github.com/vvatanabe/go-typetalk-stream/stream"

type Constructor func(stream.HandlerFunc) stream.HandlerFunc

type Chain struct {
	constructors []Constructor
}

func (c *Chain) Use(constructors ...Constructor) *Chain {
	c.constructors = append(c.constructors, constructors...)
	return c
}

func (c *Chain) Then(f stream.HandlerFunc) stream.HandlerFunc {
	for i := range c.constructors {
		f = c.constructors[len(c.constructors)-1-i](f)
	}
	return f
}
