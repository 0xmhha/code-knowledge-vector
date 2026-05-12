// TypeScript fixture for cross-language indexing tests.
// Mirrors the Go server.go in shape so the eval fixture can include
// TS-flavored intents that should hit the right symbol.

export interface Request {
  path: string;
  body?: string;
}

export interface Response {
  status: number;
  body: string;
}

// Handler routes incoming requests to a per-path callback.
export class Handler {
  private routes = new Map<string, (req: Request) => Response>();

  // register binds a path to a callback handler.
  register(path: string, fn: (req: Request) => Response): void {
    this.routes.set(path, fn);
  }

  // dispatch finds the handler for req.path and invokes it.
  dispatch(req: Request): Response {
    const fn = this.routes.get(req.path);
    if (!fn) {
      return { status: 404, body: "not found" };
    }
    return fn(req);
  }
}

// notFound is the default 404 response builder.
export const notFound = (): Response => ({ status: 404, body: "not found" });
