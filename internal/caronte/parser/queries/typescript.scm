; Caronte TypeScript tags query (Aider pattern). Captures definitions of
; functions, classes, class methods, interfaces, and interface members. Each
; definition node is captured as @definition.<kind>; its identifier as
; @name.<kind>. Reference edges (calls / imports) are a Phase E resolve concern
; (SCIP / heuristic), NOT in this defs-only query.
;
; Node types verified against the smacker/go-tree-sitter typescript grammar
; (v0.0.0-20240827094217-dd81d9e9be82): interface body is "interface_body"
; (not "object_type").

; Top-level / exported function declarations: function foo(...) {...}
(function_declaration
  name: (identifier) @name.function) @definition.function

; Arrow / function expressions bound to a const: const foo = (...) => {...}
(lexical_declaration
  (variable_declarator
    name: (identifier) @name.function
    value: [(arrow_function) (function_expression)])) @definition.function

; Class declarations: class Widget {...}
(class_declaration
  name: (type_identifier) @name.struct) @definition.struct

; Methods inside a class body.
(method_definition
  name: (property_identifier) @name.method) @definition.method

; Interface declarations: interface Renderer {...}
(interface_declaration
  name: (type_identifier) @name.interface) @definition.interface

; Interface method signatures.
(interface_declaration
  (interface_body
    (method_signature
      name: (property_identifier) @name.field) @definition.field))

; Interface property signatures.
(interface_declaration
  (interface_body
    (property_signature
      name: (property_identifier) @name.field) @definition.field))
