/**
 * A path as the app shows it: the home folder as "~", on macOS, Linux and
 * Windows, so a screenshot of an agent's row carries no one's user name.
 */
export const shortPath = (p: string) => p.replace(/^\/(Users|home)\/[^/]+/, "~").replace(/^[A-Za-z]:\\Users\\[^\\]+/i, "~");
