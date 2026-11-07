// The fork keeps its existing credential marker. Only interactive authentication
// carries JSON questions; answers are never written to credential/history storage.
export function parseKeyboardChallenge(data) {
  const challenge = JSON.parse(new TextDecoder().decode(data));
  if (
    !challenge ||
    typeof challenge.name !== "string" ||
    typeof challenge.instruction !== "string" ||
    !Array.isArray(challenge.questions) ||
    !Array.isArray(challenge.echo) ||
    challenge.questions.length !== challenge.echo.length ||
    !challenge.questions.every((q) => typeof q === "string") ||
    !challenge.echo.every((e) => typeof e === "boolean")
  ) {
    throw new Error("Invalid authentication challenge");
  }
  return challenge;
}

export function encodeKeyboardAnswers(answers) {
  const data = new TextEncoder().encode(JSON.stringify(answers));
  if (data.length > 4096) {
    throw new Error("Authentication answers are too long");
  }
  return data;
}
