package fiber

import (
	"context"
	"github.com/gofiber/fiber/v2"
	"log"
	"strconv"
)

type FiberServiceError struct {
	Msg string
}

func (r *FiberServiceError) Error() string {
	return r.Msg
}

type FiberServerExists struct {
	error
	Msg string
}

func (r *FiberServerExists) Error() string {
	return r.Msg
}

type FiberService interface {
	Create(port int, setUp func(app *fiber.App, ctx context.Context) error, forceShutdown ...bool) error
	Shutdown(port int) error
	CheckServerExists(port int) bool
}

type FiberServiceInternal interface {
	FiberService
	ShutdownAll() []error
}

type fiberServiceElement struct {
	Server     *fiber.App
	Context    context.Context
	CancelFunc context.CancelFunc
}

var _ FiberServiceInternal = &FiberServiceImpl{
	serversList: map[int]*fiberServiceElement{},
}

type FiberServiceImpl struct {
	serversList map[int]*fiberServiceElement
}

//TODO debug logs to find where stuck
func (f FiberServiceImpl) Create(port int, setUp func(app *fiber.App, ctx context.Context) error, forceShutdown ...bool) error {
	log.Print("Starting to create new fiber server")
	if f.serversList[port] != nil {
		if len(forceShutdown) > 0 && forceShutdown[0] {
			log.Printf("Stopping currently runnin server on port %d", port)
			sdErr := f.Shutdown(port)
			if sdErr != nil {
				return sdErr
			}
		} else {
			return &FiberServerExists{Msg: "Server on " + strconv.Itoa(port) + " port already presented"}
		}
	}
	log.Print("No running servers left, create a new one")
	serverCtx, cancel := context.WithCancel(context.Background())
	app := fiber.New()

	setupErr := setUp(app, serverCtx)
	if setupErr != nil {
		cancel()
		return setupErr
	}

	log.Print("Calling new go routin")
	go func() {
		if err := app.Listen(":" + strconv.Itoa(port)); err != nil {
			log.Printf("Error during go routin start %v", err)
			cancel()
			delete(f.serversList, port)
			log.Printf("Server on %v port is failed and removed from servers list: %+v", port, err)
		}
	}()

	f.serversList[port] = &fiberServiceElement{
		Server:     app,
		Context:    serverCtx,
		CancelFunc: cancel,
	}

	return nil
}

func (f FiberServiceImpl) Shutdown(port int) error {
	if f.serversList[port] == nil {
		return &FiberServiceError{Msg: "Server on " + strconv.Itoa(port) + " port not found"}
	}
	err := f.serversList[port].Server.Shutdown()
	f.serversList[port].CancelFunc()
	delete(f.serversList, port)
	return err
}

func (f FiberServiceImpl) CheckServerExists(port int) bool {
	return f.serversList[port] != nil
}

func (f FiberServiceImpl) ShutdownAll() []error {
	var errs []error
	for _, server := range f.serversList {
		err := server.Server.Shutdown()
		server.CancelFunc()
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

type FiberServiceFactory interface {
	GetInstance() FiberService
	SetInstance(fs FiberServiceInternal) []error
}

type FiberServiceFactoryImpl struct {
	instance FiberServiceInternal
}

func (f FiberServiceFactoryImpl) GetInstance() FiberService {
	return f.instance
}

func (f FiberServiceFactoryImpl) SetInstance(fs FiberServiceInternal) []error {
	// During instance change, make sure no any other server left
	var errs []error
	if f.instance != nil {
		errs = f.instance.ShutdownAll()
	}
	f.instance = fs
	return errs
}

// Set another instance of fiber service for tests
var fsf FiberServiceFactory = &FiberServiceFactoryImpl{
	&FiberServiceImpl{
		serversList: map[int]*fiberServiceElement{},
	},
}

func GetFiberServiceFactory() FiberServiceFactory {
	return fsf
}
