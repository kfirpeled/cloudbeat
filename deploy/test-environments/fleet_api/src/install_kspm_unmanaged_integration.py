#!/usr/bin/env python
"""
This script installs KSPM unmanaged integration.

The following steps are performed:
1. Create an agent policy.
2. Create a KSPM unmanaged integration.
3. Create a KSPM manifest to be deployed on a host.
"""

from pathlib import Path
from typing import Dict, Tuple
from munch import Munch
import configuration_fleet as cnfg
from api.agent_policy_api import create_agent_policy
from api.package_policy_api import create_kspm_unmanaged_integration
from api.common_api import (
    get_enrollment_token,
    get_fleet_server_host,
    create_kubernetes_manifest,
    get_package_version,
    update_package_version,
)
from loguru import logger
from utils import read_json
from state_file_manager import state_manager, PolicyState

KSPM_UNMANAGED_AGENT_POLICY = "../../../cloud/data/agent_policy_vanilla.json"
KSPM_UNMANAGED_PACKAGE_POLICY = "../../../cloud/data/package_policy_vanilla.json"
KSPM_UNMANAGED_EXPECTED_AGENTS = 2


kspm_agent_policy_data = Path(__file__).parent / KSPM_UNMANAGED_AGENT_POLICY
kspm_unmanached_pkg_policy_data = Path(__file__).parent / KSPM_UNMANAGED_PACKAGE_POLICY


def load_data() -> Tuple[Dict, Dict]:
    """Loads data.

    Returns:
        Tuple[Dict, Dict]: A tuple containing the loaded agent and package policies.
    """
    logger.info("Loading agent and package policies")
    agent_policy = read_json(json_path=kspm_agent_policy_data)
    package_policy = read_json(json_path=kspm_unmanached_pkg_policy_data)

    return agent_policy, package_policy


if __name__ == "__main__":
    # pylint: disable=duplicate-code
    package_version = get_package_version(cfg=cnfg.elk_config)
    logger.info(f"Package version: {package_version}")
    update_package_version(
        cfg=cnfg.elk_config,
        package_name="cloud_security_posture",
        package_version=package_version,
    )

    logger.info("Starting installation of KSPM integration.")
    agent_data, package_data = load_data()

    logger.info("Create agent policy")
    agent_policy_id = create_agent_policy(cfg=cnfg.elk_config, json_policy=agent_data)

    logger.info("Create KSPM unmanaged integration")
    package_policy_id = create_kspm_unmanaged_integration(
        cfg=cnfg.elk_config,
        pkg_policy=package_data,
        agent_policy_id=agent_policy_id,
    )

    state_manager.add_policy(PolicyState(agent_policy_id, package_policy_id, KSPM_UNMANAGED_EXPECTED_AGENTS, []))

    manifest_params = Munch()
    manifest_params.enrollment_token = get_enrollment_token(
        cfg=cnfg.elk_config,
        policy_id=agent_policy_id,
    )

    manifest_params.fleet_url = get_fleet_server_host(cfg=cnfg.elk_config)
    manifest_params.yaml_path = Path(__file__).parent / "kspm_unmanaged.yaml"
    manifest_params.docker_image_override = cnfg.kspm_config.docker_image_override
    logger.info("Creating KSPM unmanaged manifest")
    create_kubernetes_manifest(cfg=cnfg.elk_config, params=manifest_params)
    logger.info("Installation of KSPM integration is done")
