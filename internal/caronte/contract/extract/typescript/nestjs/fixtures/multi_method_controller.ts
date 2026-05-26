import { Controller, Get, Post, Delete, Body, Param } from '@nestjs/common';

@Controller('items')
export class ItemsController {
  @Get() findAll() { return []; }
  @Post() create(@Body() body: any) { return body; }
  @Delete(':id') remove(@Param('id') id: string) { return; }
}
