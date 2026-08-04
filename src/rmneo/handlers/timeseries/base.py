# SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
#
# SPDX-License-Identifier: Apache-2.0

from constructs import Construct
from app_common import CommonResources
from src.rmneo.handlers.timeseries.timeseries_stack import TimeseriesBase
from src.rmneo.handlers.timeseries.ts_stream_processor.stack import TimeseriesStreamProcessorBase

class ServiceBase(Construct):
    """Base/infrastructure resources for Service - includes timeseries infrastructure"""
    
    def __init__(self, scope: Construct, id: str, common_resources: CommonResources, **kwargs) -> None:
        super().__init__(scope, id, **kwargs)

        # Create timeseries base resources (DynamoDB tables)
        self.timeseries_base = TimeseriesBase(self, "TimeseriesBase", common_resources)
        
        # Create stream processor base resources (currently placeholder)
        self.timeseries_stream_processor_base = TimeseriesStreamProcessorBase(self, "TimeseriesStreamProcessorBase", common_resources)
