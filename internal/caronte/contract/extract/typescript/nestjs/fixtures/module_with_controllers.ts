import { Module } from '@nestjs/common';
import { UsersController } from './users.controller';
import { ItemsController } from './items.controller';

@Module({ controllers: [UsersController, ItemsController] })
export class AppModule {}
