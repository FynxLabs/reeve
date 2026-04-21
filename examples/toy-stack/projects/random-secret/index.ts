import * as pulumi from "@pulumi/pulumi";
import * as random from "@pulumi/random";

const cfg = new pulumi.Config();
const length = cfg.getNumber("length") ?? 12;

const pwd = new random.RandomPassword("pwd", { length, special: true });

export const password = pulumi.secret(pwd.result);
