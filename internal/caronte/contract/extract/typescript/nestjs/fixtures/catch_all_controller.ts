import { Controller, All } from '@nestjs/common';
@Controller('proxy')
export class ProxyController {
  @All() handle() { return; }
}
