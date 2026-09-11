import { parseHostPort } from "./parseHostPort";

const TARGET_SCHEME_PATTERN = /^[a-z][a-z\d+.-]*:\/\//i;

const extractTargetAuthority = (target: string): string => {
  const withoutScheme = target.replace(TARGET_SCHEME_PATTERN, "");
  const withoutProtocolRelativePrefix = withoutScheme.replace(/^\/\//, "");
  const suffixIndex = withoutProtocolRelativePrefix.search(/[/?#]/);
  const authority =
    suffixIndex === -1
      ? withoutProtocolRelativePrefix
      : withoutProtocolRelativePrefix.slice(0, suffixIndex);

  return authority.slice(authority.lastIndexOf("@") + 1);
};

export const extractPortFromTarget = (target: string): number | null => {
  const normalizedTarget = target.trim();
  if (!normalizedTarget) return null;

  return parseHostPort(extractTargetAuthority(normalizedTarget))?.port ?? null;
};
