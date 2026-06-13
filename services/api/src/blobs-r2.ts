import type { R2Bucket } from "@cloudflare/workers-types";
import type { BlobStore } from "./deps";

/**
 * R2-backed BlobStore (Cloudflare Workers). Uploads still flow through the
 * worker's PUT /v1/artifacts/:key route rather than a true presigned R2 URL:
 * R2 presigned URLs require account-scoped S3 API tokens (separate from the
 * worker's binding credentials), which we don't provision yet. A direct put
 * through the worker is equivalent for our ≤10MB artifacts; if upload volume
 * ever justifies it, swapping in real presigned URLs only changes what
 * upload_url the presign route returns — the BlobStore interface stays put.
 */
export class R2BlobStore implements BlobStore {
  constructor(private readonly bucket: R2Bucket) {}

  async put(key: string, data: Uint8Array): Promise<void> {
    await this.bucket.put(key, data);
  }

  async get(key: string): Promise<Uint8Array | null> {
    const object = await this.bucket.get(key);
    if (!object) return null;
    return new Uint8Array(await object.arrayBuffer());
  }

  async exists(key: string): Promise<boolean> {
    return (await this.bucket.head(key)) !== null;
  }
}
