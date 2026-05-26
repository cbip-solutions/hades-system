; Caronte Python tags query (Aider pattern). Captures function definitions,
; class definitions, and methods (functions nested in a class body). Each
; definition node is captured as @definition.<kind>; its identifier as
; @name.<kind>. The method-vs-function distinction is structural: a
; function_definition directly inside a class body's block is a method; a
; module-scope function_definition is a plain function. Both kinds of
; function_definition use @definition.* — pyOwnerFor supplies the class owner
; for methods via parent-walk; the capture name (@definition.method vs
; @definition.function) carries the kind.
;
; Node types verified against the smacker/go-tree-sitter python grammar
; (v0.0.0-20240827094217-dd81d9e9be82): class name is "identifier" (not
; "type_identifier"), body is "block", decorated nodes are "decorated_definition"
; with a "definition" field containing the wrapped "function_definition".

; Methods: a function_definition whose parent block belongs to a class body.
(class_definition
  body: (block
    (function_definition
      name: (identifier) @name.method) @definition.method))

; Top-level (module-scope) function definitions.
(module
  (function_definition
    name: (identifier) @name.function) @definition.function)

; Decorated top-level functions: @decorator def foo(...).
(module
  (decorated_definition
    definition: (function_definition
      name: (identifier) @name.function) @definition.function))

; Class definitions.
(class_definition
  name: (identifier) @name.struct) @definition.struct

; Decorated class definitions: @decorator class Foo.
(decorated_definition
  definition: (class_definition
    name: (identifier) @name.struct) @definition.struct)
