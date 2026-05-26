async function getHandler() { return new Response("echo"); }
async function postHandler() { return new Response("posted"); }

// `export { GET, POST }` re-export shape — exercises export_clause path.
export { getHandler as GET, postHandler as POST };
