import { Ajv } from "ajv";
import addFormats from "ajv-formats";
import runV1Schema from "../schemas/run.v1.json" with { type: "json" };

export type { AimarkRunV1 } from "./generated/run.v1";

const ajv = new Ajv({ allErrors: true, strict: false });
addFormats(ajv);

export const validateRunV1 = ajv.compile(runV1Schema);

export const RUN_SCHEMA_VERSION = "aimark.run.v1";
