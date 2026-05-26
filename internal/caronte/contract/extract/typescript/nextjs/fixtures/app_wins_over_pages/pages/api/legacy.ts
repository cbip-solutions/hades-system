import type { NextApiRequest, NextApiResponse } from "next";
export default function legacy(req: NextApiRequest, res: NextApiResponse) {
  res.status(200).end();
}
