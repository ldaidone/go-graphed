package parser

import (
	"testing"
)

func TestParse_KotlinFile_References(t *testing.T) {
	doc := parseKotlin(t, "controller.kt", `package com.acme

class OrderController(private val service: OrderService)
`)
	refs := map[string]bool{}
	for _, e := range doc.Entities {
		if e.Type == "reference" {
			refs[e.Name] = true
		}
	}
	if !refs["OrderService"] {
		t.Errorf("expected reference entity for external type OrderService, got %v", refs)
	}
	if refs["OrderController"] {
		t.Errorf("self-referential class name must not be emitted as a reference: %v", refs)
	}
}

func TestParse_CppFile_References(t *testing.T) {
	doc := parseCpp(t, "draw.cpp", `#include "circle.h"
namespace geom {
void draw() {
    Circle c;
    c.perimeter();
}
}
`)
	refs := map[string]bool{}
	for _, e := range doc.Entities {
		if e.Type == "reference" {
			refs[e.Name] = true
		}
	}
	if !refs["Circle"] {
		t.Errorf("expected reference entity for external type Circle, got %v", refs)
	}
	if refs["geom"] {
		t.Errorf("namespace name must not be emitted as a reference: %v", refs)
	}
}

func TestParse_SwiftFile_References(t *testing.T) {
	doc := parseSwift(t, "controller.swift", `import Foundation

class OrderController: BaseController {
    let service: OrderService
}
`)
	refs := map[string]bool{}
	for _, e := range doc.Entities {
		if e.Type == "reference" {
			refs[e.Name] = true
		}
	}
	for _, want := range []string{"BaseController", "OrderService"} {
		if !refs[want] {
			t.Errorf("expected reference entity for %q, got %v", want, refs)
		}
	}
	if refs["OrderController"] {
		t.Errorf("self-referential class name must not be emitted as a reference: %v", refs)
	}
}
