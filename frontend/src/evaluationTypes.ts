export type FieldAvailability = "persisted" | "raw_payload" | "realtime" | "derived" | "partial" | "missing";
export type FieldRole = "key" | "dimension" | "measure" | "timestamp" | "status" | "payload";

export type EvaluationFieldDefinition = {
  name: string;
  label: string;
  data_type: string;
  description: string;
  source_path: string;
  availability: FieldAvailability;
  role: FieldRole;
  nullable: boolean;
  sensitive: boolean;
};

export type EvaluationEntity = {
  id: string;
  label: string;
  source_system: string;
  source_kind: "database_table" | "json_array" | "realtime_api";
  physical_source: string;
  grain: string;
  description: string;
  primary_key: string[];
  join_keys: string[];
  fields: EvaluationFieldDefinition[];
};

export type EvaluationRelation = {
  id: string;
  from_entity: string;
  from_fields: string[];
  to_entity: string;
  to_fields: string[];
  cardinality: string;
  coverage: string;
  description: string;
};

export type CanonicalEvaluationField = {
  name: string;
  label: string;
  data_type: string;
  group: string;
  availability: FieldAvailability;
  source_fields: string[];
  description: string;
};

export type EvaluationDataModel = {
  catalog: {
    version: string;
    entities: EvaluationEntity[];
    relations: EvaluationRelation[];
    canonical_model: {
      id: string;
      label: string;
      grain: string;
      description: string;
      fields: CanonicalEvaluationField[];
    };
  };
  snapshot: {
    as_of: string;
    sources: Array<{
      id: string;
      label: string;
      status: "connected" | "unconfigured" | "error";
      detail: string;
    }>;
    resources: Array<{
      id: string;
      source: string;
      scope: string;
      label: string;
      physical_source: string;
      record_count: number;
      earliest_at?: string;
      latest_at?: string;
    }>;
    coverage: Array<{
      id: string;
      source: string;
      label: string;
      total: number;
      available: number;
      rate: number;
      description: string;
    }>;
    distributions: Array<{
      id: string;
      label: string;
      source: string;
      items: Array<{ name: string; secondary: string; value: number }>;
    }>;
  };
};
