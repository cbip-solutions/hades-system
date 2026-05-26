import { Controller, Get, Param } from '@nestjs/common';

@Controller('v1/users/:userId/posts')
export class UserPostsController {
  @Get(':postId/comments/:commentId')
  findComment(@Param('userId') uid: string, @Param('postId') pid: string, @Param('commentId') cid: string) {
    return {};
  }
}
