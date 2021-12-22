package proxy

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/compress"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/valyala/fasthttp"
)

type RemoteWriteProxy struct {
	RemoteWriteProxyOptions
	app *fiber.App
}

type RemoteWriteProxyOptions struct {
	listenAddr  string
	gatewayAddr string
}

type RemoteWriteProxyOption func(*RemoteWriteProxyOptions)

func (o *RemoteWriteProxyOptions) Apply(opts ...RemoteWriteProxyOption) {
	for _, op := range opts {
		op(o)
	}
}

func WithListenAddr(listenAddr string) RemoteWriteProxyOption {
	return func(o *RemoteWriteProxyOptions) {
		o.listenAddr = listenAddr
	}
}

func WithGatewayAddr(gatewayAddr string) RemoteWriteProxyOption {
	return func(o *RemoteWriteProxyOptions) {
		o.gatewayAddr = gatewayAddr
	}
}

func NewRemoteWriteProxy(opts ...RemoteWriteProxyOption) *RemoteWriteProxy {
	options := RemoteWriteProxyOptions{
		listenAddr: ":8099",
	}
	options.Apply(opts...)
	app := fiber.New(fiber.Config{
		Prefork:           false,
		StrictRouting:     false,
		AppName:           "Opni Gateway Proxy",
		ReduceMemoryUsage: false,
		Network:           "tcp4",
	})
	app.Use(logger.New(), compress.New())
	app.Get("/healthz", func(c *fiber.Ctx) error {
		return c.SendStatus(fasthttp.StatusOK)
	})

	client := &fasthttp.HostClient{
		NoDefaultUserAgentHeader: true,
		DisablePathNormalizing:   true,
		Addr:                     options.gatewayAddr,
	}
	app.Post("/api/v1/push", func(c *fiber.Ctx) error {
		req := c.Request()
		resp := c.Response()
		req.SetHost(options.gatewayAddr)
		req.Header.Del(fiber.HeaderConnection)
		if err := client.Do(req, resp); err != nil {
			return err
		}
		resp.Header.Del(fiber.HeaderConnection)
		return nil
	})

	return &RemoteWriteProxy{
		RemoteWriteProxyOptions: options,
		app:                     app,
	}
}

func (p *RemoteWriteProxy) ListenAndServe() error {
	return p.app.Listen(p.listenAddr)
}
