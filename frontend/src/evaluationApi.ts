import type { EvaluationDataModel } from "./evaluationTypes";

type Envelope<T> = { success: boolean; data?: T; error?: string };
const evaluationApiBase = `${import.meta.env.BASE_URL}evaluation-api`;

export async function fetchEvaluationDataModel(): Promise<EvaluationDataModel> {
  const response = await fetch(`${evaluationApiBase}/data-model`);
  const payload = await response.json() as Envelope<EvaluationDataModel>;
  if (!response.ok || !payload.success || !payload.data) {
    throw new Error(payload.error || `评价服务请求失败 (${response.status})`);
  }
  return payload.data;
}
