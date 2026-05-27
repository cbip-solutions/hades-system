; Caronte Go tags query (Aider pattern). Captures definitions of functions,
; methods, type specs (struct / interface / alias), interface method names,
; and struct fields (including embedded). Each definition node is captured as
; @definition.<kind>; its identifier is captured as @name.<kind> so the
; extractor can read the symbol name from the same match. Reference edges
; (calls / selectors / imports) are release track E concerns and are NOT in
; this defs-only query.

; Top-level function declarations: func Foo(...) {...}
(function_declaration
  name: (identifier) @name.function) @definition.function

; Methods with a receiver: func (r T) Foo(...) {...}
(method_declaration
  name: (field_identifier) @name.method) @definition.method

; Struct type: type T struct { ... }
(type_declaration
  (type_spec
    name: (type_identifier) @name.struct
    type: (struct_type)) ) @definition.struct

; Interface type: type R interface { ... }
(type_declaration
  (type_spec
    name: (type_identifier) @name.interface
    type: (interface_type)) ) @definition.interface

; Other named types (aliases, named non-struct/non-interface):
; type ID string  |  type Handler func(...)
(type_declaration
  (type_spec
    name: (type_identifier) @name.type) ) @definition.type

; Interface method elements: the method names declared inside an interface.
(interface_type
  (method_elem
    name: (field_identifier) @name.field) @definition.field)

; Struct fields (named): field_identifier inside a field_declaration.
(struct_type
  (field_declaration_list
    (field_declaration
      name: (field_identifier) @name.field) @definition.field))

; Embedded struct fields: a type_identifier directly in a field_declaration
; (no field name) — e.g. `io.Reader` embedded. Captured as a field too.
(struct_type
  (field_declaration_list
    (field_declaration
      type: (type_identifier) @name.field) @definition.field))
