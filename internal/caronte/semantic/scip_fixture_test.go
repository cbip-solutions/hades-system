package semantic

const scipFixtureJSON = `{
  "documents": [
    {
      "relative_path": "src/app/widget.ts",
      "language": "typescript",
      "symbols": [
        {
          "symbol": "scip-typescript npm app 1.0.0 src/app/widget.ts/Widget#",
          "relationships": [
            { "symbol": "scip-typescript npm app 1.0.0 src/app/widget.ts/Renderer#", "is_implementation": true }
          ]
        }
      ],
      "occurrences": [
        { "symbol": "scip-typescript npm app 1.0.0 src/app/widget.ts/Renderer#", "symbol_roles": 1, "range": [0, 10, 0, 18] },
        { "symbol": "scip-typescript npm app 1.0.0 src/app/widget.ts/Widget#", "symbol_roles": 1, "range": [4, 13, 4, 19] },
        { "symbol": "scip-typescript npm app 1.0.0 src/app/widget.ts/Widget#render().", "symbol_roles": 1, "range": [5, 2, 5, 8] }
      ]
    },
    {
      "relative_path": "src/app/main.ts",
      "language": "typescript",
      "symbols": [],
      "occurrences": [
        { "symbol": "scip-typescript npm app 1.0.0 src/app/main.ts/run().", "symbol_roles": 1, "range": [1, 9, 1, 12] },
        {
          "symbol": "scip-typescript npm app 1.0.0 src/app/widget.ts/Widget#render().",
          "symbol_roles": 8,
          "range": [9, 4, 9, 20],
          "enclosing_symbol": "scip-typescript npm app 1.0.0 src/app/main.ts/run()."
        }
      ]
    }
  ]
}`
