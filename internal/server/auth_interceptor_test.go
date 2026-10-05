package server

import (
	"context"
	"testing"

	grpclib "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	pb "github.com/ai-process/llm-proxy/gen/llmproxy/v1"
	"github.com/ai-process/llm-proxy/internal/apikeys"
	"github.com/ai-process/llm-proxy/internal/proxydb"
)

type fakeStore struct {
	rows map[string]*proxydb.APIKey
}

func (f *fakeStore) GetAPIKeyByLookup(_ context.Context, lookup string) (*proxydb.APIKey, error) {
	row, ok := f.rows[lookup]
	if !ok {
		return nil, proxydb.ErrNotFound
	}
	return row, nil
}

func mintKey(t *testing.T, store *fakeStore, name string, scopes ...string) string {
	t.Helper()
	plain, row, err := apikeys.Generate(name, scopes)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	store.rows[row.KeyLookup] = row
	return plain
}

func callWithKey(t *testing.T, verifier *apikeys.Verifier, fullMethod, bearer string) (*apikeys.Identity, error) {
	t.Helper()
	ctx := context.Background()
	if bearer != "" {
		ctx = metadata.NewIncomingContext(ctx, metadata.Pairs("authorization", bearer))
	}
	var gotIdentity *apikeys.Identity
	handler := func(ctx context.Context, _ any) (any, error) {
		gotIdentity = apikeys.IdentityFrom(ctx)
		return "ok", nil
	}
	_, err := authUnaryInterceptor(verifier)(ctx, nil,
		&grpclib.UnaryServerInfo{FullMethod: fullMethod}, handler)
	return gotIdentity, err
}

func TestUnmappedMethodFailsClosed(t *testing.T) {
	store := &fakeStore{rows: map[string]*proxydb.APIKey{}}
	admin := mintKey(t, store, "root", apikeys.ScopeAdmin)
	v := apikeys.NewVerifier(store, "")

	// Even an admin key is refused on a method nobody assigned a scope to.
	_, err := callWithKey(t, v, "/llmproxy.v1.LLMProxyService/BrandNewMethod", "Bearer "+admin)
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("code = %v, want PermissionDenied", status.Code(err))
	}
}

func TestMissingAuthorizationIsUnauthenticated(t *testing.T) {
	v := apikeys.NewVerifier(&fakeStore{rows: map[string]*proxydb.APIKey{}}, "")
	_, err := callWithKey(t, v, method(pb.LLMProxyService_GenerateText_FullMethodName), "")
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("code = %v, want Unauthenticated", status.Code(err))
	}
}

func TestWrongScopeIsPermissionDenied(t *testing.T) {
	store := &fakeStore{rows: map[string]*proxydb.APIKey{}}
	genKey := mintKey(t, store, "generation", apikeys.ScopeGenerate)
	v := apikeys.NewVerifier(store, "")

	_, err := callWithKey(t, v, method(pb.LLMProxyAdminService_MintAPIKey_FullMethodName), "Bearer "+genKey)
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("code = %v, want PermissionDenied", status.Code(err))
	}
}

func TestValidKeyPassesAndInjectsIdentity(t *testing.T) {
	store := &fakeStore{rows: map[string]*proxydb.APIKey{}}
	genKey := mintKey(t, store, "generation", apikeys.ScopeGenerate)
	v := apikeys.NewVerifier(store, "")

	id, err := callWithKey(t, v, method(pb.LLMProxyService_GenerateText_FullMethodName), "Bearer "+genKey)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if id == nil || id.Name != "generation" {
		t.Fatalf("identity = %+v, want caller name injected for handlers", id)
	}
}

func TestEveryProtoMethodHasAScope(t *testing.T) {
	// Walked from the generated descriptors, not a hand-written list: a list
	// silently stops covering the methods nobody remembered to add to it, which
	// is the exact mistake this test exists to catch.
	for _, desc := range []grpclib.ServiceDesc{pb.LLMProxyService_ServiceDesc, pb.LLMProxyAdminService_ServiceDesc} {
		for _, m := range desc.Methods {
			full := "/" + desc.ServiceName + "/" + m.MethodName
			if _, ok := methodScopes[full]; !ok {
				t.Errorf("method %s has no scope configured", full)
			}
		}
	}
}

// The handler tests call ProxyServer.Judge directly, so only this one proves a
// generate-scoped client actually reaches it through the interceptor.
func TestJudgeIsReachableWithAGenerateKey(t *testing.T) {
	store := &fakeStore{rows: map[string]*proxydb.APIKey{}}
	client := mintKey(t, store, "chatbot", apikeys.ScopeGenerate)
	v := apikeys.NewVerifier(store, "")

	id, err := callWithKey(t, v, pb.LLMProxyService_Judge_FullMethodName, "Bearer "+client)
	if err != nil {
		t.Fatalf("Judge refused for a generate key: %v", err)
	}
	if id == nil || id.Name != "chatbot" {
		t.Errorf("identity = %+v, want the calling client", id)
	}
}
