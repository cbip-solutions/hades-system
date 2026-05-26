; Caronte Rust tags query (Aider pattern). Captures free functions, struct/enum
; definitions, trait definitions, impl-block methods, and struct fields. Each
; definition node is @definition.<kind>; its identifier @name.<kind>. A trait
; maps to KindInterface; a struct/enum maps to KindStruct (Phase A's frozen kind
; set). The impl-method owner (the type the impl is for) is derived in
; rustOwnerFor by walking to the enclosing impl_item.
;
; Node types verified against the smacker/go-tree-sitter rust grammar
; (v0.0.0-20240827094217-dd81d9e9be82): trait body uses declaration_list;
; trait method SIGNATURES are "function_signature_item" (abstract, no body);
; trait DEFAULT methods are "function_item" (with body); impl-block methods are
; always "function_item". The free-function pattern is anchored to source_file.

; Free functions: fn foo(...) {...} at the crate root or module level.
; Anchored to source_file so impl/trait methods are NOT double-captured here.
(source_file
  (function_item
    name: (identifier) @name.function) @definition.function)

; Methods inside an impl block (both plain impl and impl Trait for Type).
(impl_item
  body: (declaration_list
    (function_item
      name: (identifier) @name.method) @definition.method))

; Abstract trait method signatures (no body): fn render(&self);
(trait_item
  body: (declaration_list
    (function_signature_item
      name: (identifier) @name.method) @definition.method))

; Default trait method implementations (with body): fn color(&self) { ... }
(trait_item
  body: (declaration_list
    (function_item
      name: (identifier) @name.method) @definition.method))

; Struct definitions.
(struct_item
  name: (type_identifier) @name.struct) @definition.struct

; Enum definitions (mapped to KindStruct — a named aggregate type).
(enum_item
  name: (type_identifier) @name.struct) @definition.struct

; Trait definitions (mapped to KindInterface).
(trait_item
  name: (type_identifier) @name.interface) @definition.interface

; Named struct fields.
(struct_item
  body: (field_declaration_list
    (field_declaration
      name: (field_identifier) @name.field) @definition.field))
