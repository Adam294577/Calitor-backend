package permission

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func ctxWithPerms(perms ...string) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	if len(perms) > 0 {
		ps := make([]interface{}, len(perms))
		for i, p := range perms {
			ps[i] = p
		}
		c.Set("Permissions", ps)
	}
	return c
}

// ---- Struct value fields ----

type bankReq struct {
	AccountNo string `json:"account_no"`
	Name      string `json:"name"`
}

func TestStrip_ValueStruct_NoPermission(t *testing.T) {
	c := ctxWithPerms()
	req := bankReq{AccountNo: "A-999", Name: "New"}
	stripped := StripMasterCodeFields(c, &req, "account_no")
	if !stripped {
		t.Fatalf("expected stripped=true")
	}
	if req.AccountNo != "" {
		t.Fatalf("expected AccountNo zeroed, got %q", req.AccountNo)
	}
	if req.Name != "New" {
		t.Fatalf("Name should be untouched, got %q", req.Name)
	}
}

func TestStrip_ValueStruct_WithPermission(t *testing.T) {
	c := ctxWithPerms(MasterCodePermission)
	req := bankReq{AccountNo: "A-999", Name: "New"}
	stripped := StripMasterCodeFields(c, &req, "account_no")
	if stripped {
		t.Fatalf("expected stripped=false when permission present")
	}
	if req.AccountNo != "A-999" {
		t.Fatalf("AccountNo should be untouched, got %q", req.AccountNo)
	}
}

func TestStrip_ValueStruct_AlreadyZero(t *testing.T) {
	c := ctxWithPerms()
	req := bankReq{AccountNo: "", Name: "X"}
	stripped := StripMasterCodeFields(c, &req, "account_no")
	if stripped {
		t.Fatalf("expected stripped=false when field already zero")
	}
}

// ---- Struct pointer fields (customer/vendor pattern) ----

type vendorReq struct {
	Code *string `json:"code"`
	Name *string `json:"name"`
}

func strPtr(s string) *string { return &s }

func TestStrip_PointerField_NoPermission(t *testing.T) {
	c := ctxWithPerms()
	req := vendorReq{Code: strPtr("V-001"), Name: strPtr("Acme")}
	stripped := StripMasterCodeFields(c, &req, "code")
	if !stripped {
		t.Fatalf("expected stripped=true")
	}
	if req.Code != nil {
		t.Fatalf("expected Code=nil, got %v", *req.Code)
	}
	if req.Name == nil || *req.Name != "Acme" {
		t.Fatalf("Name should be untouched")
	}
}

func TestStrip_PointerField_AlreadyNil(t *testing.T) {
	c := ctxWithPerms()
	req := vendorReq{Code: nil, Name: strPtr("Acme")}
	stripped := StripMasterCodeFields(c, &req, "code")
	if stripped {
		t.Fatalf("expected stripped=false when already nil")
	}
}

// ---- Map (product/member pattern) ----

func TestStrip_Map_NoPermission(t *testing.T) {
	c := ctxWithPerms()
	req := map[string]interface{}{
		"model_code": "M-01",
		"name":       "T-shirt",
	}
	stripped := StripMasterCodeFields(c, req, "model_code")
	if !stripped {
		t.Fatalf("expected stripped=true")
	}
	if _, ok := req["model_code"]; ok {
		t.Fatalf("expected model_code deleted")
	}
	if req["name"] != "T-shirt" {
		t.Fatalf("name should be preserved")
	}
}

func TestStrip_Map_WithPermission(t *testing.T) {
	c := ctxWithPerms(MasterCodePermission)
	req := map[string]interface{}{"model_code": "M-01"}
	stripped := StripMasterCodeFields(c, req, "model_code")
	if stripped || req["model_code"] != "M-01" {
		t.Fatalf("with permission should keep field")
	}
}

func TestStrip_Map_KeyMissing(t *testing.T) {
	c := ctxWithPerms()
	req := map[string]interface{}{"name": "X"}
	stripped := StripMasterCodeFields(c, req, "model_code")
	if stripped {
		t.Fatalf("missing key should not count as stripped")
	}
}

// ---- Multi-field ----

type multiReq struct {
	Code      string `json:"code"`
	AccountNo string `json:"account_no"`
	Name      string `json:"name"`
}

func TestStrip_MultipleFields(t *testing.T) {
	c := ctxWithPerms()
	req := multiReq{Code: "C1", AccountNo: "A1", Name: "N"}
	stripped := StripMasterCodeFields(c, &req, "code", "account_no")
	if !stripped {
		t.Fatalf("expected stripped=true")
	}
	if req.Code != "" || req.AccountNo != "" {
		t.Fatalf("expected both code fields zeroed, got %+v", req)
	}
	if req.Name != "N" {
		t.Fatalf("Name must be untouched")
	}
}

// ---- Edge cases ----

func TestStrip_UnknownJSONTag(t *testing.T) {
	c := ctxWithPerms()
	req := bankReq{AccountNo: "A-1"}
	stripped := StripMasterCodeFields(c, &req, "nonexistent")
	if stripped {
		t.Fatalf("unknown field should not strip")
	}
	if req.AccountNo != "A-1" {
		t.Fatalf("existing field should not be touched")
	}
}

func TestStrip_NilReq(t *testing.T) {
	c := ctxWithPerms()
	if StripMasterCodeFields(c, nil, "code") {
		t.Fatalf("nil req should not strip")
	}
}

func TestStrip_NonStructPointer(t *testing.T) {
	c := ctxWithPerms()
	s := "hi"
	if StripMasterCodeFields(c, &s, "code") {
		t.Fatalf("non-struct pointer should not strip")
	}
}

func TestStrip_ValueNotPointer(t *testing.T) {
	// passing struct by value — can't mutate; helper should bail
	c := ctxWithPerms()
	req := bankReq{AccountNo: "A-1"}
	if StripMasterCodeFields(c, req, "account_no") {
		t.Fatalf("non-pointer struct should not strip")
	}
	if req.AccountNo != "A-1" {
		t.Fatalf("value should not be touched via copy")
	}
}

// ---- Cache sanity: same type twice uses cached field map ----

func TestStrip_CacheReuse(t *testing.T) {
	c := ctxWithPerms()
	r1 := bankReq{AccountNo: "X"}
	r2 := bankReq{AccountNo: "Y"}
	_ = StripMasterCodeFields(c, &r1, "account_no")
	_ = StripMasterCodeFields(c, &r2, "account_no")
	if r1.AccountNo != "" || r2.AccountNo != "" {
		t.Fatalf("both should be stripped (cache did not break behavior)")
	}
}
