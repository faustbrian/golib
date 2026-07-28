package siblings_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	authenticationhttp "github.com/faustbrian/golib/pkg/authentication/authhttp"
	"github.com/faustbrian/golib/pkg/authentication/bearer"
	authorization "github.com/faustbrian/golib/pkg/authorization"
	authorizationhttp "github.com/faustbrian/golib/pkg/authorization/authhttp"
	"github.com/faustbrian/golib/pkg/authorization/authn"
	httpclient "github.com/faustbrian/golib/pkg/http-client"
	jsonapi "github.com/faustbrian/golib/pkg/jsonapi"
	jsonrpc "github.com/faustbrian/golib/pkg/jsonrpc"
	log "github.com/faustbrian/golib/pkg/log"
	"github.com/faustbrian/golib/pkg/log/handler/capture"
	openapi "github.com/faustbrian/golib/pkg/openapi"
	"github.com/faustbrian/golib/pkg/openapi/parse"
	router "github.com/faustbrian/golib/pkg/router"
	"github.com/faustbrian/golib/pkg/service/serverhttp"
)

type allowAuthorizer struct{}

func (allowAuthorizer) Decide(
	context.Context,
	authorization.Request,
) (authorization.Decision, error) {
	return authorization.Decision{Outcome: authorization.Allow}, nil
}

func TestServiceComposesHTTPClientLoggingAuthenticationAuthorizationAndJSONAPI(
	t *testing.T,
) {
	t.Parallel()

	logs := capture.New()
	logger, err := log.New(logs)
	if err != nil {
		t.Fatalf("log.New() error = %v", err)
	}
	extractor, err := authenticationhttp.NewExtractor(
		authenticationhttp.BearerAuthorization(),
	)
	if err != nil {
		t.Fatalf("authenticationhttp.NewExtractor() error = %v", err)
	}
	authenticator, err := bearer.New(bearer.ValidatorFunc(func(
		context.Context,
		string,
	) (authentication.Principal, error) {
		return authentication.NewPrincipal(authentication.PrincipalSpec{
			Subject: "fixture-service",
			Method:  "bearer",
		})
	}))
	if err != nil {
		t.Fatalf("bearer.New() error = %v", err)
	}
	authenticate, err := authenticationhttp.NewMiddleware(
		extractor,
		authenticator,
	)
	if err != nil {
		t.Fatalf("authenticationhttp.NewMiddleware() error = %v", err)
	}

	application := http.HandlerFunc(func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		principal, ok := authentication.PrincipalFromContext(request.Context())
		if !ok {
			http.Error(writer, "missing principal", http.StatusInternalServerError)

			return
		}
		if _, ok := authorizationhttp.DecisionFromContext(request.Context()); !ok {
			http.Error(writer, "missing decision", http.StatusInternalServerError)

			return
		}
		payload, marshalErr := jsonapi.Marshal(jsonapi.Document{
			Data: jsonapi.ResourceData(jsonapi.ResourceObject{
				Type: "articles",
				ID:   "article-1",
				Attributes: jsonapi.Attributes{
					"viewer": principal.Subject(),
				},
			}),
		})
		if marshalErr != nil {
			http.Error(writer, "encoding failed", http.StatusInternalServerError)

			return
		}
		logger.InfoContext(request.Context(), "article served")
		writer.Header().Set("Content-Type", jsonapi.MediaTypeJSONAPI)
		_, _ = writer.Write(payload)
	})
	authorize, err := authorizationhttp.NewHandler(
		allowAuthorizer{},
		func(request *http.Request) (authorization.Request, error) {
			principal, _ := authentication.PrincipalFromContext(request.Context())
			subject, subjectErr := authn.Subject(principal, authn.Config{
				Kind: authorization.SubjectServiceAccount,
			})
			if subjectErr != nil {
				return authorization.Request{}, subjectErr
			}

			return authorization.Request{
				Subject:  subject,
				Action:   "article.read",
				Resource: authorization.Resource{Type: "article", ID: "article-1"},
			}, nil
		},
		application,
	)
	if err != nil {
		t.Fatalf("authorizationhttp.NewHandler() error = %v", err)
	}
	builder := router.New()
	if err = builder.Register(router.Route{
		Name: "articles.show", Methods: []string{http.MethodGet},
		Path: "/articles/{id}", Handler: authorize,
	}); err != nil {
		t.Fatalf("router.Register() error = %v", err)
	}
	routes, err := builder.Compile()
	if err != nil {
		t.Fatalf("router.Compile() error = %v", err)
	}
	handler, err := serverhttp.Chain(routes, serverhttp.Recover(), authenticate)
	if err != nil {
		t.Fatalf("serverhttp.Chain() error = %v", err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := httpclient.New(httpclient.Config{
		Transport: server.Client().Transport,
	})
	if err != nil {
		t.Fatalf("httpclient.New() error = %v", err)
	}
	t.Cleanup(func() {
		if closeErr := client.Close(); closeErr != nil {
			t.Errorf("httpclient.Close() error = %v", closeErr)
		}
	})
	request, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		server.URL+"/articles/article-1",
		nil,
	)
	if err != nil {
		t.Fatalf("http.NewRequestWithContext() error = %v", err)
	}
	request.Header.Set("Authorization", "Bearer fixture-token")
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("httpclient.Do() error = %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("io.ReadAll() error = %v", err)
	}
	if response.StatusCode != http.StatusOK ||
		response.Header.Get("Content-Type") != jsonapi.MediaTypeJSONAPI ||
		!bytes.Contains(body, []byte(`"viewer":"fixture-service"`)) {
		t.Fatalf(
			"response = (%d, %q, %s)",
			response.StatusCode,
			response.Header.Get("Content-Type"),
			body,
		)
	}
	if record, ok := logs.Last(); !ok || record.Message != "article served" {
		t.Fatalf("last log record = (%v, %t)", record, ok)
	}
}

func TestServiceComposesJSONRPCAndGeneratedOpenAPIHandlers(t *testing.T) {
	t.Parallel()

	registry := jsonrpc.NewRegistry()
	if err := registry.Register(
		"ping",
		func(context.Context, json.RawMessage) (any, error) {
			return "pong", nil
		},
	); err != nil {
		t.Fatalf("jsonrpc.Register() error = %v", err)
	}
	rpc := jsonrpc.NewHTTPHandler(jsonrpc.NewDispatcher(registry))
	rpcHandler, err := serverhttp.Chain(rpc, serverhttp.Recover())
	if err != nil {
		t.Fatalf("serverhttp.Chain(JSON-RPC) error = %v", err)
	}
	rpcRecorder := httptest.NewRecorder()
	rpcRequest := httptest.NewRequest(
		http.MethodPost,
		"/rpc",
		strings.NewReader(`{"jsonrpc":"2.0","method":"ping","id":1}`),
	)
	rpcRequest.Header.Set("Content-Type", "application/json")
	rpcHandler.ServeHTTP(
		rpcRecorder,
		rpcRequest,
	)
	if rpcRecorder.Code != http.StatusOK ||
		!strings.Contains(rpcRecorder.Body.String(), `"result":"pong"`) {
		t.Fatalf(
			"JSON-RPC response = (%d, %s)",
			rpcRecorder.Code,
			rpcRecorder.Body.String(),
		)
	}

	document, err := openapi.ParseJSON(
		context.Background(),
		strings.NewReader(`{
			"openapi":"3.1.2",
			"info":{"title":"Fixture API","version":"1"},
			"paths":{"/pets/{id}":{"get":{"responses":{"200":{"description":"ok"}}}}}
		}`),
		parse.DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("openapi.ParseJSON() error = %v", err)
	}
	if document.SpecificationVersion().String() != "3.1.2" {
		t.Fatalf(
			"OpenAPI version = %s",
			document.SpecificationVersion(),
		)
	}
	generated := generatedPetHandler{
		getPet: func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(http.StatusNoContent)
		},
	}
	generatedHandler, err := serverhttp.Chain(generated, serverhttp.Recover())
	if err != nil {
		t.Fatalf("serverhttp.Chain(generated handler) error = %v", err)
	}
	generatedRecorder := httptest.NewRecorder()
	generatedHandler.ServeHTTP(
		generatedRecorder,
		httptest.NewRequest(http.MethodGet, "/pets/pet-1", nil),
	)
	if generatedRecorder.Code != http.StatusNoContent {
		t.Fatalf("generated handler status = %d", generatedRecorder.Code)
	}
}

// generatedPetHandler represents the net/http surface emitted by OpenAPI code
// generators without making service depend on a generator runtime.
type generatedPetHandler struct {
	getPet http.HandlerFunc
}

func (handler generatedPetHandler) ServeHTTP(
	writer http.ResponseWriter,
	request *http.Request,
) {
	handler.getPet(writer, request)
}
