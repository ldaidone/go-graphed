package parser

import (
	"testing"
)

func TestParse_KotlinFile_PackageEntity(t *testing.T) {
	doc := parseKotlin(t, "App.kt", `package com.acme

class App
`)
	byName := map[string]string{}
	byPath := map[string]string{}
	for _, e := range doc.Entities {
		byName[e.Name] = e.Type
		if e.Type == "package" {
			byPath[e.Name] = e.Metadata["package_path"]
		}
	}
	if byName["com.acme"] != "package" {
		t.Errorf("expected package entity com.acme, got %v", byName)
	}
	if byPath["com.acme"] != "com.acme" {
		t.Errorf("package_path = %q, want com.acme", byPath["com.acme"])
	}
}

func TestParse_JavaFile_PackageEntity(t *testing.T) {
	doc := parseJava(t, "App.java", `package com.acme;

class App {}
`)
	byName := map[string]string{}
	for _, e := range doc.Entities {
		if e.Type == "package" {
			byName[e.Name] = e.Metadata["package_path"]
		}
	}
	if byName["com.acme"] != "com.acme" {
		t.Errorf("expected package entity com.acme with package_path, got %v", byName)
	}
}

func TestParse_PhpFile_PackageEntity(t *testing.T) {
	doc := parsePHP(t, "App.php", `<?php
namespace Acme\Orders;

class App {}
`)
	byName := map[string]string{}
	for _, e := range doc.Entities {
		if e.Type == "package" {
			byName[e.Name] = e.Metadata["package_path"]
		}
	}
	if byName["Acme\\Orders"] != "Acme\\Orders" {
		t.Errorf("expected package entity Acme\\Orders, got %v", byName)
	}
}

func TestParse_CSharpFile_PackageEntity(t *testing.T) {
	doc := parseCSharp(t, "App.cs", `namespace Acme.Orders
{
    class App { }
}
`)
	byName := map[string]string{}
	for _, e := range doc.Entities {
		if e.Type == "package" {
			byName[e.Name] = e.Metadata["package_path"]
		}
	}
	if byName["Acme.Orders"] != "Acme.Orders" {
		t.Errorf("expected package entity Acme.Orders, got %v", byName)
	}
}

func TestParse_ElixirFile_PackageEntity(t *testing.T) {
	doc := parseElixir(t, "shapes.ex", `defmodule Geometry.Shapes do
  def area(r) do
    r * r
  end
end
`)
	byName := map[string]string{}
	for _, e := range doc.Entities {
		if e.Type == "package" {
			byName[e.Name] = e.Metadata["package_path"]
		}
	}
	if byName["Geometry"] != "Geometry" {
		t.Errorf("expected package entity Geometry (first module segment), got %v", byName)
	}
}
