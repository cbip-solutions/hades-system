export const GET = async (req: Request) => new Response("ok");
export const HEAD = async (req: Request) => new Response(null);

// Ensure the extractor distinguishes uppercased-method-named const from an
// unrelated all-caps const (false-positive guard).
export const GET_LIMIT = 100;
