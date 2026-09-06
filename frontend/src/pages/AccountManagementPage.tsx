import { PageHeader } from "../components/Common";
import AccountManagementView from "./accounts/AccountManagementView";
import "./ShippingPoliciesPage.css";

export default function AccountManagementPage() {
  return <><PageHeader title="OMS 账号管理" /><AccountManagementView /></>;
}
