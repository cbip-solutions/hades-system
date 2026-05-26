import { Controller, Get, Param } from '@nestjs/common';
import { ApiOperation, ApiTags } from '@nestjs/swagger';

@ApiTags('billing')
@Controller('invoices')
export class InvoicesController {
  @Get(':id')
  @ApiOperation({ operationId: 'getInvoiceById', summary: 'Fetch one invoice' })
  findOne(@Param('id') id: string) { return { id }; }
}
