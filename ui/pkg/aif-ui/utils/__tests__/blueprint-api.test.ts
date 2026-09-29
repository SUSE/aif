import { describe, it, expect } from "vitest";
import { blueprintIcon, sourceFor, blueprintCRName, slugifyBlueprintName } from "../blueprint-api";
import type { Blueprint } from "../../types/blueprint-types";

function mockBlueprint(overrides: Partial<Blueprint["spec"]> = {}): Blueprint {
  return {
    apiVersion: "ai-factory.suse.com/v1alpha1",
    kind: "Blueprint",
    metadata: { name: "test-bp-1-0-0" },
    spec: {
      displayName: "Test Blueprint",
      version: "1.0.0",
      components: [],
      ...overrides,
    },
  };
}

describe("blueprintIcon", () => {
  it("returns undefined when icon is not provided or empty", () => {
    expect(blueprintIcon(undefined)).toBeUndefined();
    expect(blueprintIcon(null)).toBeUndefined();
    expect(blueprintIcon(mockBlueprint({ icon: undefined }))).toBeUndefined();
    expect(blueprintIcon(mockBlueprint({ icon: "" }))).toBeUndefined();
    expect(blueprintIcon(mockBlueprint({ icon: "   " }))).toBeUndefined();
  });

  it("accepts valid raster data URIs", () => {
    const png = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==";
    expect(blueprintIcon(mockBlueprint({ icon: png }))).toBe(png);

    const webp = "data:image/webp;base64,UklGRiQAAABXRUJQVlA4IBgAAAAwAQCdASoBAAEAAQAcJaQAA3AA/v3AgAA=";
    expect(blueprintIcon(mockBlueprint({ icon: webp }))).toBe(webp);
  });

  it("accepts valid SVG data URIs", () => {
    const svg = "data:image/svg+xml;base64,PHN2ZyB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciPjwvc3ZnPg==";
    expect(blueprintIcon(mockBlueprint({ icon: svg }))).toBe(svg);
  });

  it("accepts valid http and https URLs", () => {
    const https = "https://partner.example.com/logo.svg";
    expect(blueprintIcon(mockBlueprint({ icon: https }))).toBe(https);

    const http = "http://partner.example.com/logo.png";
    expect(blueprintIcon(mockBlueprint({ icon: http }))).toBe(http);
  });

  it("trims whitespace from URLs", () => {
    const https = "  https://partner.example.com/logo.svg  ";
    expect(blueprintIcon(mockBlueprint({ icon: https }))).toBe("https://partner.example.com/logo.svg");
  });

  it("rejects dangerous or unsupported schemes", () => {
    expect(blueprintIcon(mockBlueprint({ icon: "javascript:alert(1)" }))).toBeUndefined();
    expect(blueprintIcon(mockBlueprint({ icon: "data:text/html;base64,PHNjcmlwdD4=" }))).toBeUndefined();
    expect(blueprintIcon(mockBlueprint({ icon: "ftp://example.com/logo.png" }))).toBeUndefined();
    expect(blueprintIcon(mockBlueprint({ icon: "file:///etc/passwd" }))).toBeUndefined();
  });
});

describe("sourceFor", () => {
  it("defaults to Custom when source is undefined", () => {
    expect(sourceFor(mockBlueprint())).toBe("Custom");
  });

  it("returns the specified origin", () => {
    expect(sourceFor(mockBlueprint({ source: "SUSE" }))).toBe("SUSE");
    expect(sourceFor(mockBlueprint({ source: "Nvidia" }))).toBe("Nvidia");
    expect(sourceFor(mockBlueprint({ source: "Custom" }))).toBe("Custom");
  });
});

describe("slugifyBlueprintName and blueprintCRName", () => {
  it("slugifies blueprint names properly", () => {
    expect(slugifyBlueprintName("My AI Stack")).toBe("my-ai-stack");
    expect(slugifyBlueprintName("  --RAG Service--  ")).toBe("rag-service");
  });

  it("derives the CR name matching backend", () => {
    expect(blueprintCRName("My AI Stack", "1.0.0")).toBe("my-ai-stack-1-0-0");
    expect(blueprintCRName("My AI Stack", "2.1.0+build.4")).toBe("my-ai-stack-2-1-0");
  });
});
