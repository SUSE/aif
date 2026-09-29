import { CATALOG_DEFAULT_NAME } from "../types/catalog-types";
import { DNS_LABEL_PATTERN } from "../types/blueprint-types";

export const CATALOG_NAME_MAX_LENGTH = 40;

export type CatalogNameErrorCode = "required" | "tooLong" | "invalid" | "reserved" | "duplicate";

export interface CatalogNameValidationResult {
  valid: boolean;
  error?: string;
  code?: CatalogNameErrorCode;
}

/**
 * Validates a blueprint catalog name according to Kubernetes / CRD specifications:
 * - Must be non-empty (required)
 * - Maximum length: 40 characters
 * - Must match DNS-1123 label pattern: ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$
 * - Cannot be "suse-default" (reserved for built-in catalog)
 * - Must be unique among configured catalogs
 */
export function validateCatalogName(
  name: string | undefined | null,
  otherExistingNames: string[] = [],
): CatalogNameValidationResult {
  const trimmed = (name || "").trim();

  if (!trimmed) {
    return {
      valid: false,
      code:  "required",
      error: "Catalog name is required",
    };
  }

  if (trimmed.length > CATALOG_NAME_MAX_LENGTH) {
    return {
      valid: false,
      code:  "tooLong",
      error: `Catalog name must be ${ CATALOG_NAME_MAX_LENGTH } characters or less`,
    };
  }

  if (!DNS_LABEL_PATTERN.test(trimmed)) {
    return {
      valid: false,
      code:  "invalid",
      error: "Catalog name must be lowercase alphanumeric and hyphens only, and must start and end with an alphanumeric character",
    };
  }

  if (trimmed === CATALOG_DEFAULT_NAME) {
    return {
      valid: false,
      code:  "reserved",
      error: `"${ CATALOG_DEFAULT_NAME }" is reserved for the default catalog`,
    };
  }

  const normalizedOthers = otherExistingNames.map((n) => (n || "").trim());
  if (normalizedOthers.includes(trimmed)) {
    return {
      valid: false,
      code:  "duplicate",
      error: "Catalog name must be unique",
    };
  }

  return { valid: true };
}
