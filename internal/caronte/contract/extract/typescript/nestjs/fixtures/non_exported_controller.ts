import { Controller, Get } from '@nestjs/common';

// Non-exported controller — exercises the I-5 review sister-test: the
// walker must process bare `class_declaration` nodes (not just
// `export_statement > class_declaration`). A regression that
// re-introduced double-emission via walking export_statement would only
// be caught for export-wrapped classes; this fixture catches the
// non-export side of the symmetry.
@Controller('private')
class PrivateController {
  @Get('hello')
  hello() { return 'hi'; }
}
