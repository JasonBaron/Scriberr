// Tags added by Scriberr itself get a distinct look: sensitive and pii
// warn, youtube marks the source.
export function flagTagClass(tag: string): string {
  switch (tag) {
    case "pii":
      return "border-red-300 text-red-600 dark:border-red-800 dark:text-red-400";
    case "sensitive":
      return "border-amber-300 text-amber-700 dark:border-amber-800 dark:text-amber-400";
    case "youtube":
      return "border-rose-200 text-rose-600 dark:border-rose-900 dark:text-rose-400";
    default:
      return "";
  }
}
