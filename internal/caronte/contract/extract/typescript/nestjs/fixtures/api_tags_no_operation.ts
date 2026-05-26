import { Controller, Get } from '@nestjs/common';
import { ApiTags } from '@nestjs/swagger';

// @ApiTags is collected as classTags by the extractor but per the
// implementation comment "intentionally unused in this phase" — the doc-
// hint surface is Phase F's responsibility. This fixture exercises the
// "no @ApiOperation, only @ApiTags" path so the sister-test
// TestEndpointsApiTagsNotInHandlerNodeID can pin the claim that tags do
// NOT leak into HandlerNodeID today.
@ApiTags('billing')
@Controller('reports')
export class ReportsController {
  @Get('summary')
  summary() {
    return { ok: true };
  }
}
