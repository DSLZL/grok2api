"""
Statsig ID generator for reverse interfaces.
"""

import base64
import random
import string

from app.core.config import get_config
from app.core.logger import logger


STATIC_STATSIG_ID = "ZTpUeXBlRXJyb3I6IENhbm5vdCByZWFkIHByb3BlcnRpZXMgb2YgdW5kZWZpbmVkIChyZWFkaW5nICdjaGlsZE5vZGVzJyk="


class StatsigGenerator:
    """Statsig ID generator for reverse interfaces."""

    @staticmethod
    def _rand(length: int, alphanumeric: bool = False) -> str:
        """Generate random string."""
        chars = (
            string.ascii_lowercase + string.digits
            if alphanumeric
            else string.ascii_lowercase
        )
        return "".join(random.choices(chars, k=length))

    @staticmethod
    def gen_id() -> str:
        """
        Generate Statsig ID.

        Returns:
            Base64 encoded ID.
        """
        dynamic = get_config("chat.dynamic_statsig")

        # Dynamic Statsig ID
        if dynamic:
            logger.debug("Generating dynamic Statsig ID")
            
            if random.choice([True, False]):
                rand = StatsigGenerator._rand(5, alphanumeric=True)
                message = f"e:TypeError: Cannot read properties of null (reading 'children['{rand}']')"
            else:
                rand = StatsigGenerator._rand(10)
                message = (
                    f"e:TypeError: Cannot read properties of undefined (reading '{rand}')"
                )

            return base64.b64encode(message.encode()).decode()

        # Static Statsig ID
        logger.debug("Generating static Statsig ID")
        return STATIC_STATSIG_ID


__all__ = ["StatsigGenerator"]
