import { describe, it, expect } from "vitest";
import { validateCatalogName, CATALOG_NAME_MAX_LENGTH } from "../catalog";

describe("validateCatalogName", () => {
  it("accepts valid DNS-1123 catalog names", () => {
    expect(validateCatalogName("acme")).toEqual({ valid: true });
    expect(validateCatalogName("partner-123")).toEqual({ valid: true });
    expect(validateCatalogName("catalog-a-b-c")).toEqual({ valid: true });
    expect(validateCatalogName("c")).toEqual({ valid: true });
  });

  it("trims leading and trailing whitespace", () => {
    expect(validateCatalogName("  acme-catalog  ")).toEqual({ valid: true });
  });

  it("rejects empty or whitespace-only name", () => {
    expect(validateCatalogName("")).toEqual({
      valid: false,
      code:  "required",
      error: "Catalog name is required",
    });
    expect(validateCatalogName("   ")).toEqual({
      valid: false,
      code:  "required",
      error: "Catalog name is required",
    });
    expect(validateCatalogName(undefined)).toEqual({
      valid: false,
      code:  "required",
      error: "Catalog name is required",
    });
    expect(validateCatalogName(null)).toEqual({
      valid: false,
      code:  "required",
      error: "Catalog name is required",
    });
  });

  it("rejects names exceeding 40 characters", () => {
    const exactly40 = "a".repeat(40);
    expect(validateCatalogName(exactly40)).toEqual({ valid: true });

    const tooLong41 = "a".repeat(41);
    expect(validateCatalogName(tooLong41)).toEqual({
      valid: false,
      code:  "tooLong",
      error: `Catalog name must be ${CATALOG_NAME_MAX_LENGTH} characters or less`,
    });
  });

  it("rejects uppercase letters", () => {
    expect(validateCatalogName("Acme")).toEqual({
      valid: false,
      code:  "invalid",
      error: "Catalog name must be lowercase alphanumeric and hyphens only, and must start and end with an alphanumeric character",
    });
    expect(validateCatalogName("my-Catalog")).toEqual({
      valid: false,
      code:  "invalid",
      error: "Catalog name must be lowercase alphanumeric and hyphens only, and must start and end with an alphanumeric character",
    });
  });

  it("rejects spaces and special characters", () => {
    expect(validateCatalogName("my catalog")).toEqual({
      valid: false,
      code:  "invalid",
      error: "Catalog name must be lowercase alphanumeric and hyphens only, and must start and end with an alphanumeric character",
    });
    expect(validateCatalogName("my_catalog")).toEqual({
      valid: false,
      code:  "invalid",
      error: "Catalog name must be lowercase alphanumeric and hyphens only, and must start and end with an alphanumeric character",
    });
    expect(validateCatalogName("my.catalog")).toEqual({
      valid: false,
      code:  "invalid",
      error: "Catalog name must be lowercase alphanumeric and hyphens only, and must start and end with an alphanumeric character",
    });
  });

  it("rejects names starting or ending with a hyphen", () => {
    expect(validateCatalogName("-acme")).toEqual({
      valid: false,
      code:  "invalid",
      error: "Catalog name must be lowercase alphanumeric and hyphens only, and must start and end with an alphanumeric character",
    });
    expect(validateCatalogName("acme-")).toEqual({
      valid: false,
      code:  "invalid",
      error: "Catalog name must be lowercase alphanumeric and hyphens only, and must start and end with an alphanumeric character",
    });
    expect(validateCatalogName("-")).toEqual({
      valid: false,
      code:  "invalid",
      error: "Catalog name must be lowercase alphanumeric and hyphens only, and must start and end with an alphanumeric character",
    });
  });

  it("rejects reserved name suse-default", () => {
    expect(validateCatalogName("suse-default")).toEqual({
      valid: false,
      code:  "reserved",
      error: `"suse-default" is reserved for the default catalog`,
    });
  });

  it("rejects duplicate catalog names", () => {
    const existing = ["catalog-1", "partner-alpha", "partner-beta"];
    expect(validateCatalogName("partner-alpha", existing)).toEqual({
      valid: false,
      code:  "duplicate",
      error: "Catalog name must be unique",
    });
    expect(validateCatalogName("  partner-beta  ", existing)).toEqual({
      valid: false,
      code:  "duplicate",
      error: "Catalog name must be unique",
    });
    expect(validateCatalogName("partner-gamma", existing)).toEqual({
      valid: true,
    });
  });
});
