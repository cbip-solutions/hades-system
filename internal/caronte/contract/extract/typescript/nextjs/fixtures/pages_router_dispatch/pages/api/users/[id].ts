import type { NextApiRequest, NextApiResponse } from "next";
export default function handler(req: NextApiRequest, res: NextApiResponse) {
  if (req.method === "GET") return res.json({ id: req.query.id });
  if (req.method === "POST") return res.status(201).json({});
  return res.status(405).end();
}
