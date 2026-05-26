# Express

Fast, unopinionated, minimalist web framework for Node.js.

## Installation

```bash
npm install express
```

## Quick Start

```js
const express = require('express');
const app = express();

app.get('/', (req, res) => res.send('Hello'));
app.listen(3000);
```
