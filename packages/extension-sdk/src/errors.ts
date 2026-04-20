export class NotImplementedError extends Error {
  readonly method: string;
  readonly spec: string;
  constructor(method: string, spec: string) {
      super(`@go-pi/extension-sdk: ${method} not implemented (deferred to spec ${spec})`);
    this.name = "NotImplementedError";
    this.method = method;
    this.spec = spec;
  }
}

export class CapabilityDeniedError extends Error {
  readonly capability: string;
  readonly reason?: string;
  constructor(capability: string, reason?: string) {
      super(`@go-pi/extension-sdk: capability denied: ${capability}${reason ? ` (${reason})` : ""}`);
    this.name = "CapabilityDeniedError";
    this.capability = capability;
    this.reason = reason;
  }
}
